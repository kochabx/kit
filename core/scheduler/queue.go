package scheduler

import (
	"context"

	"github.com/kochabx/kit/core/scheduler/internal/model"
	"github.com/kochabx/kit/core/scheduler/internal/store"
)

const DefaultQueue = "default"

// Queue is a stable scoped view. Lists never choose a queue implicitly.
type Queue struct{ store *store.Queue }

func (s *clientState) Queue(name string) (*Queue, error) {
	if !validName.MatchString(name) {
		return nil, ErrInvalidArgument
	}
	q, err := s.lookupQueue(name)
	if err != nil {
		return nil, err
	}
	return &Queue{store: q.store}, nil
}

func (q *Queue) Name() string {
	if q == nil || q.store == nil {
		return ""
	}
	return q.store.Name()
}

func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	if q == nil || q.store == nil {
		return Stats{}, ErrInvalidArgument
	}
	return q.store.Stats(ctx)
}

func (q *Queue) DeadJobs(ctx context.Context, query DeadQuery) (Page[*Job], error) {
	if q == nil || q.store == nil {
		return Page[*Job]{}, ErrInvalidArgument
	}

	if !isValidScan(query.Count) {
		return Page[*Job]{}, ErrInvalidArgument
	}
	return q.store.DeadJobs(ctx, query)
}

func (q *Queue) RunningJobs(ctx context.Context, scan Scan) (Page[*Job], error) {
	if q == nil || q.store == nil {
		return Page[*Job]{}, ErrInvalidArgument
	}

	if !isValidScan(scan.Count) {
		return Page[*Job]{}, ErrInvalidArgument
	}
	return q.store.RunningJobs(ctx, scan)
}

func (q *Queue) Schedules(ctx context.Context, query ScheduleQuery) (Page[*Schedule], error) {
	if q == nil || q.store == nil {
		return Page[*Schedule]{}, ErrInvalidArgument
	}

	if !isValidScan(query.Count) || !isValidScheduleState(query.State) {
		return Page[*Schedule]{}, ErrInvalidArgument
	}
	return q.store.ListSchedules(ctx, query)
}

// Scan resumes an index traversal. Count is a Redis scan hint, not a page size.
type Scan = model.Scan

// Page is an unordered scan result. Continue until NextCursor is zero, even
// when Items is empty. Concurrent index updates may yield duplicates.
type Page[T any] = model.Page[T]

func isValidScan(count int64) bool {
	return count > 0 && count <= 1000
}

func isValidScheduleState(state ScheduleState) bool {
	return state == "" || state == ScheduleActive || state == SchedulePaused || state == ScheduleCancelled || state == ScheduleInvalid
}
