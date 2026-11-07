package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Namespace provides namespace-wide access using a borrowed Redis client.
type Namespace struct {
	Client  redis.UniversalClient
	Timeout time.Duration
	readyMu sync.Mutex
	ready   atomic.Bool
	Keys    NamespaceKeys
}

// Queue owns queue-local indexes and transport operations. Namespace access is
// deliberately shared; neither adapter owns or closes the Redis client.
type Queue struct {
	*Namespace
	Keys                QueueKeys
	ReadMaxBytes        int64
	ClaimIdle           time.Duration
	DeadRetention       time.Duration
	CompletionRetention time.Duration
	SubmissionRetention time.Duration
	HistoryLimit        int
	HistoryTTL          time.Duration
}

// ServerTimeMillis returns the authoritative Redis clock in Unix milliseconds.
// Redis is the shared ordering authority, so scheduler nodes never compare
// distributed deadlines using their local wall clocks.
func (s *Namespace) ServerTimeMillis(ctx context.Context) (int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	// TIME without a key could select a different Cluster node. The anchor key
	// routes this script to the same namespace slot as every state transition.
	return Time.Run(ctx, s.Client, []string{s.Keys.Clock()}).Int64()
}

func (s *Namespace) Ping(ctx context.Context) error {
	ctx, cancel := s.commandContext(ctx)
	defer cancel()
	if err := s.CheckCapabilities(ctx); err != nil {
		return err
	}
	return s.Client.Ping(ctx).Err()
}

func (s *Namespace) HeartbeatBatch(ctx context.Context, requests []LeaseRequest) []LeaseResult {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	const BatchSize = 256
	if len(requests) > BatchSize {
		out := make([]LeaseResult, 0, len(requests))
		for batch := range slices.Chunk(requests, BatchSize) {
			out = append(out, s.HeartbeatBatch(ctx, batch)...)
		}
		return out
	}
	pipe := s.Client.Pipeline()
	cmds := make([]*redis.Cmd, len(requests))
	for i, r := range requests {
		if r.Renew {
			cmds[i] = RenewLease.Eval(ctx, pipe, []string{s.Keys.Job(r.JobID)}, r.Token, CeilDurationMillis(r.Lease))
		} else {
			cmds[i] = CheckCancellation.Eval(ctx, pipe, []string{s.Keys.Job(r.JobID)}, r.Token)
		}
	}
	_, _ = pipe.Exec(ctx)
	out := make([]LeaseResult, len(requests))
	for i, cmd := range cmds {
		value, err := cmd.Int64()
		if value == -1 && err == nil {
			err = ErrJobCancelled
		}
		out[i] = LeaseResult{OK: value == 1, Err: err}
	}
	return out
}

func (s *Namespace) GetJob(ctx context.Context, id string) (*Job, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	m, err := s.Client.HGetAll(ctx, s.Keys.Job(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	return decodeJob(id, m), nil
}

func (s *Namespace) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	m, err := s.Client.HGetAll(ctx, s.Keys.Schedule(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	return decodeSchedule(id, m), nil
}

func (s *Namespace) GetScheduleRecord(ctx context.Context, id string) (ScheduleRecord, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	m, err := s.Client.HGetAll(ctx, s.Keys.Schedule(id)).Result()
	if err != nil {
		return ScheduleRecord{}, err
	}
	if len(m) == 0 {
		return ScheduleRecord{}, ErrNotFound
	}
	return ScheduleRecord{Schedule: *decodeSchedule(id, m)}, nil
}

// getJobs preserves index order, skips expired entities, and propagates storage
// errors rather than silently returning partial results. Reads share one pipeline.
func (s *Namespace) getJobs(ctx context.Context, ids []string) ([]*Job, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	if len(ids) == 0 {
		return nil, nil
	}
	pipe := s.Client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, s.Keys.Job(id))
	}
	_, _ = pipe.Exec(ctx)
	jobs := make([]*Job, 0, len(ids))
	for i, cmd := range cmds {
		fields, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		if len(fields) == 0 {
			continue
		}
		jobs = append(jobs, decodeJob(ids[i], fields))
	}
	return jobs, nil
}

// checkCapabilities rejects unsupported servers before any scheduler mutation.
// Failures are not cached, so a transient outage does not poison the instance.
func (s *Namespace) CheckCapabilities(ctx context.Context) error {
	if s.ready.Load() {
		return nil
	}
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	if s.ready.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	info, err := s.Client.Info(ctx, "server").Result()
	if err != nil {
		return err
	}
	version := ""
	for line := range strings.SplitSeq(info, "\n") {
		if after, ok := strings.CutPrefix(line, "redis_version:"); ok {
			version = strings.TrimSpace(after)
		}
	}
	parts := strings.Split(version, ".")
	major, minor := 0, 0
	if len(parts) >= 2 {
		major, _ = strconv.Atoi(parts[0])
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 8 || (major == 8 && minor < 10) {
		return fmt.Errorf("Redis 8.10+ required, server reports %q: %w", version, ErrUnsupportedRedis)
	}
	commands, err := s.Client.Do(ctx, "COMMAND", "INFO", "XACKDEL", "XNACK", "XCFGSET").Slice()
	if err != nil {
		return err
	}
	if len(commands) != 3 {
		return ErrUnsupportedRedis
	}
	for _, command := range commands {
		if command == nil {
			return ErrUnsupportedRedis
		}
	}
	s.ready.Store(true)
	return nil
}

func (s *Namespace) commandContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(parent, timeout)
}

// New borrows a Redis client. All namespace key construction stays in this package.
func New(client redis.UniversalClient, namespace string, timeout time.Duration) *Namespace {
	return &Namespace{Client: client, Keys: NewNamespaceKeys(namespace), Timeout: timeout}
}

type QueueOptions struct {
	HistoryLimit                                                       int
	HistoryTTL                                                         time.Duration
	ReadMaxBytes                                                       int64
	ClaimIdle, DeadRetention, CompletionRetention, SubmissionRetention time.Duration
}

func (s *Namespace) Queue(name string, options QueueOptions) *Queue {
	return &Queue{Namespace: s, Keys: s.Keys.Queue(name), ReadMaxBytes: options.ReadMaxBytes, ClaimIdle: options.ClaimIdle, DeadRetention: options.DeadRetention, CompletionRetention: options.CompletionRetention, SubmissionRetention: options.SubmissionRetention, HistoryLimit: options.HistoryLimit, HistoryTTL: options.HistoryTTL}
}
func (s *Queue) Name() string { return s.Keys.Name }
func (s *Namespace) JobQueue(ctx context.Context, id string) (string, error) {
	return s.entityQueue(ctx, s.Keys.Job(id))
}
func (s *Namespace) ScheduleQueue(ctx context.Context, id string) (string, error) {
	return s.entityQueue(ctx, s.Keys.Schedule(id))
}
func (s *Namespace) entityQueue(ctx context.Context, key string) (string, error) {
	ctx, cancel := s.commandContext(ctx)
	defer cancel()
	name, err := s.Client.HGet(ctx, key, "queue").Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return name, err
}
