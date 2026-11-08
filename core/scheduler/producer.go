package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kochabx/kit/core/scheduler/internal/model"
	"github.com/kochabx/kit/core/scheduler/internal/store"
)

// taskClient binds immutable task metadata to one engine. It is safe for
// concurrent producers; handlers must be registered before the engine runs.
type taskClient[T any] struct {
	scheduler *clientState
	task      Task[T]
	store     *store.Queue
}

func (t Task[T]) Fingerprint() string {
	return t.fingerprint
}

func (t Task[T]) Name() string {
	return t.name
}

func (t Task[T]) Queue() string {
	return t.queue
}

func (t Task[T]) bind(s *clientState) (*taskClient[T], error) {
	if s == nil || t.fingerprint == "" {
		return nil, ErrInvalidArgument
	}
	if s.closed.Load() {
		return nil, ErrClosed
	}
	q, err := s.lookupQueue(t.queue)
	if err != nil {
		return nil, err
	}
	return &taskClient[T]{scheduler: s, task: t, store: q.store}, nil
}

func (t *taskClient[T]) Enqueue(ctx context.Context, payload T, opts ...EnqueueOption) (*Job, error) {
	if !t.isValid() {
		return nil, ErrInvalidArgument
	}

	s, d := t.scheduler, t.task
	record, err := t.prepareJob(payload, opts...)
	if err != nil {
		return nil, err
	}
	job, err := t.store.EnqueueJob(ctx, record)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return job, err
		}
		return nil, err
	}
	s.observe(EventEnqueued, ctx, Event{Queue: d.queue, JobID: record.ID, TaskName: d.name})
	return job, nil
}

func (t *taskClient[T]) prepareJob(payload T, opts ...EnqueueOption) (store.EnqueueRecord, error) {
	s, d := t.scheduler, t.task
	if s.closed.Load() {
		return store.EnqueueRecord{}, ErrClosed
	}
	b, err := t.encodePayload(payload)
	if err != nil {
		return store.EnqueueRecord{}, err
	}
	o := enqueueOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.delay < 0 {
		return store.EnqueueRecord{}, fmt.Errorf("delay cannot be negative: %w", ErrInvalidArgument)
	}
	if len(o.uniqueKey) > 256 || (o.uniqueKey != "" && o.uniqueTTL <= 0) {
		return store.EnqueueRecord{}, fmt.Errorf("invalid unique job option: %w", ErrInvalidArgument)
	}
	if o.expiresAfter < 0 {
		return store.EnqueueRecord{}, fmt.Errorf("invalid job expiration option: %w", ErrInvalidArgument)
	}
	fingerprint := d.fingerprint
	id := o.id
	if id != "" && !validName.MatchString(id) {
		return store.EnqueueRecord{}, ErrInvalidArgument
	}
	if id == "" {
		id = uuid.NewString()
	}
	maxAttempts := policyAttemptLimit(d.policy)
	return store.EnqueueRecord{
		ID:           id,
		TaskName:     d.name,
		Payload:      b,
		RunAt:        o.runAt,
		Delay:        o.delay,
		MaxAttempts:  maxAttempts,
		UniqueKey:    o.uniqueKey,
		UniqueTTL:    o.uniqueTTL,
		ExpiresAt:    o.expiresAt,
		ExpiresAfter: o.expiresAfter,
		Fingerprint:  fingerprint,
		Version:      d.version, Policy: d.policy,
	}, nil
}

func (t *taskClient[T]) EnqueueBatch(ctx context.Context, payloads []T, opts ...EnqueueOption) []EnqueueResult {
	if !t.isValid() {
		results := make([]EnqueueResult, len(payloads))
		for i := range results {
			results[i].Err = ErrInvalidArgument
		}
		return results
	}

	results := make([]EnqueueResult, len(payloads))
	records := make([]store.EnqueueRecord, 0, min(len(payloads), store.MaxBatchJobs))
	indexes := make([]int, 0, cap(records))
	bytes := 0
	flush := func() {
		submitted := t.store.EnqueueBatch(ctx, records)
		for i, index := range indexes {
			results[index] = submitted[i]
			if submitted[i].Err == nil {
				job := submitted[i].Job
				t.scheduler.observe(EventEnqueued, ctx, Event{Queue: job.Queue, JobID: job.ID, TaskName: job.TaskName})
			}
		}
		clear(records)
		records, indexes, bytes = records[:0], indexes[:0], 0
	}
	for i, payload := range payloads {
		if err := ctx.Err(); err != nil {
			flush()
			for j := i; j < len(results); j++ {
				results[j].Err = err
			}
			return results
		}
		record, err := t.prepareJob(payload, opts...)
		if err != nil {
			results[i].Err = err
			continue
		}
		if len(records) > 0 && (len(records) == store.MaxBatchJobs || bytes+len(record.Payload) > store.MaxBatchPayloadBytes) {
			flush()
		}
		records = append(records, record)
		indexes = append(indexes, i)
		bytes += len(record.Payload)
	}
	flush()
	return results
}

func (t *taskClient[T]) Schedule(ctx context.Context, payload T, spec CronSpec) (*Schedule, error) {
	if !t.isValid() {
		return nil, ErrInvalidArgument
	}

	s, d := t.scheduler, t.task
	expression := spec.Expression
	if s.closed.Load() {
		return nil, ErrClosed
	}
	parsed, location, err := parseCron(spec)
	if err != nil {
		return nil, err
	}
	b, err := t.encodePayload(payload)
	if err != nil {
		return nil, err
	}
	fingerprint := d.fingerprint
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil {
		return nil, fmt.Errorf("read redis time: %w", err)
	}
	nextMillis, err := nextRunMillis(parsed, location, now)
	if err != nil {
		return nil, err
	}
	next := time.UnixMilli(nextMillis)
	schedule := Schedule{
		Queue:      d.queue,
		ID:         uuid.NewString(),
		TaskName:   d.name,
		Expression: expression,
		TimeZone:   location.String(),
		NextAt:     next,
		State:      ScheduleActive,
	}
	if err := t.store.CreateSchedule(ctx, schedule, b, policyAttemptLimit(d.policy), fingerprint, d.version, d.policy); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (t *taskClient[T]) encodePayload(payload T) ([]byte, error) {
	if t.scheduler.closed.Load() {
		return nil, ErrClosed
	}
	data, err := t.task.codec.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w: %w", ErrInvalidArgument, err)
	}
	if len(data) > t.scheduler.config.MaxPayloadBytes {
		return nil, fmt.Errorf("payload is %d bytes; limit is %d: %w", len(data), t.scheduler.config.MaxPayloadBytes, ErrInvalidArgument)
	}
	return data, nil
}

type EnqueueResult = model.EnqueueResult

func (t *taskClient[T]) isValid() bool {
	return t != nil && t.scheduler != nil && t.store != nil && t.task.fingerprint != ""
}
