package scheduler

import (
	"context"
	"errors"
	"time"
)

type Stats struct {
	Scheduled, Ready, Pending, Running, Dead, CronSchedules int64
	ObserverDropped                                         uint64
	OldestScheduledAge                                      time.Duration
	OldestPendingIdle                                       time.Duration
}

func (s *Scheduler) Stats(ctx context.Context) (Stats, error) {
	stats, err := s.store.stats(ctx)
	stats.ObserverDropped = s.observerDropped.Load()
	return stats, err
}
func (s *Scheduler) DeadJobs(ctx context.Context, offset, count int64) ([]*Job, error) {
	if offset < 0 || count <= 0 || count > 1000 {
		return nil, ErrInvalidArgument
	}
	return s.store.deadJobs(ctx, offset, count)
}
func (s *Scheduler) RunningJobs(ctx context.Context, offset, count int64) ([]*Job, error) {
	if offset < 0 || count <= 0 || count > 1000 {
		return nil, ErrInvalidArgument
	}
	return s.store.runningJobs(ctx, offset, count)
}
func (s *Scheduler) RetryDead(ctx context.Context, id string) error {
	return s.store.retryDead(ctx, id)
}
func (s *Scheduler) DeleteDead(ctx context.Context, id string) error {
	return s.store.deleteDead(ctx, id)
}

type DeadResult struct {
	ID  string
	Err error
}

func (s *Scheduler) RetryDeadBatch(ctx context.Context, ids []string) []DeadResult {
	out := make([]DeadResult, len(ids))
	for i, id := range ids {
		out[i] = DeadResult{ID: id, Err: s.RetryDead(ctx, id)}
		if ctx.Err() != nil {
			for j := i + 1; j < len(ids); j++ {
				out[j] = DeadResult{ID: ids[j], Err: ctx.Err()}
			}
			break
		}
		if i+1 < len(ids) && s.config.ReplayInterval > 0 && !waitContext(ctx, s.config.ReplayInterval) {
			for j := i + 1; j < len(ids); j++ {
				out[j] = DeadResult{ID: ids[j], Err: ctx.Err()}
			}
			break
		}
	}
	return out
}

type DeadQuery struct {
	Offset, Limit int64
	Type          string
}

func (s *Scheduler) QueryDead(ctx context.Context, q DeadQuery) ([]*Job, error) {
	if q.Offset < 0 || q.Limit <= 0 || q.Limit > 1000 {
		return nil, ErrInvalidArgument
	}
	return s.store.queryDead(ctx, q)
}

type EnqueueResult struct {
	Job *Job
	Err error
}

func BatchEnqueue[T any](s *Scheduler, ctx context.Context, d Definition[T], payloads []T, opts ...EnqueueOption) []EnqueueResult {
	results := make([]EnqueueResult, len(payloads))
	records := make([]enqueueRecord, 0, len(payloads))
	indexes := make([]int, 0, len(payloads))
	for i, payload := range payloads {
		record, err := prepareEnqueue(s, d, payload, opts...)
		if err != nil {
			results[i].Err = err
			continue
		}
		records = append(records, record)
		indexes = append(indexes, i)
	}
	ids, states, runAts, createdAts, expiresAts, errs := s.store.batchEnqueue(ctx, records)
	for i, index := range indexes {
		results[index].Err = errs[i]
		if errs[i] == nil {
			r := records[i]
			results[index].Job = &Job{ID: r.id, Type: r.typ, State: states[i], Payload: r.payload, MaxAttempts: r.maxAttempts, RunAt: runAts[i], CreatedAt: createdAts[i], ExpiresAt: expiresAts[i], UniqueKey: r.uniqueKey, Definition: r.definition}
			s.observe(eventEnqueued, ctx, Event{JobID: r.id, Type: r.typ})
		} else if errors.Is(errs[i], ErrDuplicate) {
			job, getErr := s.store.get(ctx, ids[i])
			results[index].Job = job
			if getErr != nil {
				results[index].Err = errors.Join(ErrDuplicate, getErr)
			}
		}
	}
	return results
}
