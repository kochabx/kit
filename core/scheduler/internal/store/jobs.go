package store

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type EnqueueRecord struct {
	ID, TaskName string
	Payload      []byte
	RunAt        time.Time
	Delay        time.Duration
	MaxAttempts  int
	UniqueKey    string
	UniqueTTL    time.Duration
	ExpiresAt    time.Time
	ExpiresAfter time.Duration
	Fingerprint  string
	Version      string
	Policy       ExecutionPolicy
}

func (s *Queue) EnqueueJob(ctx context.Context, r EnqueueRecord) (*Job, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	if err := s.CheckCapabilities(ctx); err != nil {
		return nil, err
	}
	unique := s.Keys.UniquePlaceholder()
	if r.UniqueKey != "" {
		unique = s.Keys.Unique(r.UniqueKey)
	}
	result, err := Enqueue.Run(ctx, s.Client,
		[]string{s.Keys.Job(r.ID), s.Keys.Scheduled(), unique, s.Keys.Ready(), s.Keys.Submission(r.ID)},
		r.ID, r.TaskName, r.Payload, TimeMillis(r.RunAt), r.MaxAttempts, r.UniqueKey, CeilDurationMillis(r.UniqueTTL), s.Keys.JobPrefix(), CeilDurationMillis(r.Delay), r.Fingerprint, TimeMillis(r.ExpiresAt), CeilDurationMillis(r.ExpiresAfter), s.Keys.Name, r.Version, encodePolicy(r.Policy), s.Keys.Name+":"+submissionSignature(r), CeilDurationMillis(s.SubmissionRetention), uuid.NewString()).Result()
	if err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
	values, ok := result.([]any)
	if !ok || len(values) < 3 {
		return nil, fmt.Errorf("enqueue job: unexpected redis response")
	}
	code, _ := values[0].(int64)
	if code == -1 {
		return nil, ErrConflict
	}
	id, _ := values[1].(string)
	if code == 0 {
		job, getErr := s.GetJob(ctx, id)
		if errors.Is(getErr, ErrNotFound) {
			return nil, ErrDuplicate
		}
		if getErr != nil {
			return nil, errors.Join(ErrDuplicate, getErr)
		}
		return job, ErrDuplicate
	}
	if len(values) < 6 {
		return nil, fmt.Errorf("enqueue job: incomplete redis response")
	}
	state, _ := values[2].(string)
	runAt, _ := values[3].(int64)
	createdAt, _ := values[4].(int64)
	expiresAt, _ := values[5].(int64)
	return &Job{
		Version: r.Version, Policy: r.Policy,
		Queue:       s.Keys.Name,
		ID:          id,
		TaskName:    r.TaskName,
		State:       JobState(state),
		Payload:     r.Payload,
		MaxAttempts: r.MaxAttempts,
		RunAt:       time.UnixMilli(runAt),
		CreatedAt:   time.UnixMilli(createdAt),
		ExpiresAt:   ParseTimeMillis(strconv.FormatInt(expiresAt, 10)),
		UniqueKey:   r.UniqueKey,
		Fingerprint: r.Fingerprint,
	}, nil
}

const (
	MaxBatchJobs         = 256
	MaxBatchPayloadBytes = 4 << 20
)

func (s *Queue) EnqueueBatch(ctx context.Context, records []EnqueueRecord) []EnqueueResult {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	results := make([]EnqueueResult, len(records))
	for start := 0; start < len(records); {
		end, bytes := start, 0
		for end < len(records) && end-start < MaxBatchJobs {
			size := len(records[end].Payload)
			if end > start && bytes+size > MaxBatchPayloadBytes {
				break
			}
			bytes += size
			end++
		}
		copy(results[start:end], s.enqueueChunk(ctx, records[start:end]))
		start = end
	}
	return results
}

func (s *Queue) enqueueChunk(ctx context.Context, records []EnqueueRecord) []EnqueueResult {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	results := make([]EnqueueResult, len(records))
	if len(records) == 0 {
		return results
	}
	if err := s.CheckCapabilities(ctx); err != nil {
		for i := range results {
			results[i].Err = err
		}
		return results
	}
	pipe := s.Client.Pipeline()
	cmds := make([]*redis.Cmd, len(records))
	for i, r := range records {
		unique := s.Keys.UniquePlaceholder()
		if r.UniqueKey != "" {
			unique = s.Keys.Unique(r.UniqueKey)
		}
		cmds[i] = Enqueue.Eval(ctx, pipe, []string{s.Keys.Job(r.ID), s.Keys.Scheduled(), unique, s.Keys.Ready(), s.Keys.Submission(r.ID)}, r.ID, r.TaskName, r.Payload, TimeMillis(r.RunAt), r.MaxAttempts, r.UniqueKey, CeilDurationMillis(r.UniqueTTL), s.Keys.JobPrefix(), CeilDurationMillis(r.Delay), r.Fingerprint, TimeMillis(r.ExpiresAt), CeilDurationMillis(r.ExpiresAfter), s.Keys.Name, r.Version, encodePolicy(r.Policy), s.Keys.Name+":"+submissionSignature(r), CeilDurationMillis(s.SubmissionRetention), uuid.NewString())
	}
	_, _ = pipe.Exec(ctx)
	duplicateRows := make(map[string][]int)
	for i, cmd := range cmds {
		value, err := cmd.Result()
		if err != nil {
			results[i].Err = fmt.Errorf("enqueue job: %w", err)
			continue
		}
		values, ok := value.([]any)
		if !ok || len(values) < 3 {
			results[i].Err = fmt.Errorf("invalid enqueue response")
			continue
		}
		code, ok := values[0].(int64)
		if !ok {
			results[i].Err = fmt.Errorf("invalid enqueue status")
			continue
		}
		id, ok := values[1].(string)
		if !ok {
			results[i].Err = fmt.Errorf("invalid enqueue ID")
			continue
		}
		if code == -1 {
			results[i].Err = ErrConflict
			continue
		}
		if code == 0 {
			results[i].Err = ErrDuplicate
			duplicateRows[id] = append(duplicateRows[id], i)
			continue
		}
		if len(values) != 6 {
			results[i].Err = fmt.Errorf("incomplete enqueue response")
			continue
		}
		state, stateOK := values[2].(string)
		runAt, runOK := values[3].(int64)
		createdAt, createdOK := values[4].(int64)
		expiresAt, expiresOK := values[5].(int64)
		if !stateOK || !runOK || !createdOK || !expiresOK {
			results[i].Err = fmt.Errorf("invalid enqueue fields")
			continue
		}
		r := records[i]
		job := &Job{
			Version: r.Version, Policy: r.Policy,
			Queue:       s.Keys.Name,
			ID:          id,
			TaskName:    r.TaskName,
			State:       JobState(state),
			Payload:     r.Payload,
			MaxAttempts: r.MaxAttempts,
			RunAt:       time.UnixMilli(runAt),
			CreatedAt:   time.UnixMilli(createdAt),
			UniqueKey:   r.UniqueKey,
			Fingerprint: r.Fingerprint,
		}
		if expiresAt > 0 {
			job.ExpiresAt = time.UnixMilli(expiresAt)
		}
		results[i].Job = job
	}
	if len(duplicateRows) > 0 {
		reads := s.Client.Pipeline()
		commands := make(map[string]*redis.MapStringStringCmd, len(duplicateRows))
		for id := range duplicateRows {
			commands[id] = reads.HGetAll(ctx, s.Keys.Job(id))
		}
		_, _ = reads.Exec(ctx)
		for id, cmd := range commands {
			fields, err := cmd.Result()
			for _, index := range duplicateRows[id] {
				if err != nil {
					results[index].Err = errors.Join(ErrDuplicate, err)
				} else if len(fields) > 0 {
					results[index].Job = decodeJob(id, fields)
				}
			}
		}
	}
	return results
}

func (s *Queue) DispatchJobs(ctx context.Context, batch int64, retention time.Duration) (int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := Dispatch.Run(ctx, s.Client,
		[]string{s.Keys.Scheduled(), s.Keys.Ready()},
		s.Keys.JobPrefix(), batch, CeilDurationMillis(retention), s.Keys.Name).Int64()
	if err != nil {
		return 0, fmt.Errorf("dispatch jobs: %w", err)
	}
	return result, nil
}

func (s *Queue) CancelJob(ctx context.Context, id string, retention time.Duration) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := Cancel.Run(ctx, s.Client, []string{s.Keys.Job(id), s.Keys.Scheduled()}, CeilDurationMillis(retention)).Int64()
	if err != nil {
		return err
	}
	switch result {
	case -1:
		return ErrNotFound
	case 0:
		return ErrInvalidState
	default:
		return nil
	}
}

func submissionSignature(r EnqueueRecord) string {
	b, _ := json.Marshal(struct {
		ID, Task, Version, Fingerprint string
		PayloadHash                    [32]byte
		RunAt, ExpiresAt               int64
		Delay, ExpiresAfter            int64
		Unique                         string
		UniqueTTL                      int64
		Policy                         ExecutionPolicy
	}{r.ID, r.TaskName, r.Version, r.Fingerprint, sha256.Sum256(r.Payload), TimeMillis(r.RunAt), TimeMillis(r.ExpiresAt), int64(r.Delay), int64(r.ExpiresAfter), r.UniqueKey, int64(r.UniqueTTL), r.Policy}, json.Deterministic(true))
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
