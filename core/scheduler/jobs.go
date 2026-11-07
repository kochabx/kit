package scheduler

import (
	"context"

	"github.com/kochabx/kit/core/scheduler/internal/model"
)

// Jobs operates on jobs by namespace-wide ID; mutations use persisted queue ownership.
type Jobs struct{ scheduler *clientState }

func (s *clientState) Jobs() Jobs { return Jobs{scheduler: s} }

func (j Jobs) Get(ctx context.Context, id string) (*Job, error) {
	if j.scheduler == nil || id == "" {
		return nil, ErrInvalidArgument
	}

	s := j.scheduler
	return s.namespace.GetJob(ctx, id)
}

func (j Jobs) Cancel(ctx context.Context, id string) error {
	if j.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := j.scheduler

	q, err := s.lookupJobQueue(ctx, id)
	if err != nil {
		return err
	}
	if err := q.store.CancelJob(ctx, id, s.config.Retention.Cancelled); err != nil {
		return err
	}

	return nil
}

func (j Jobs) Retry(ctx context.Context, id string) error {
	if j.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := j.scheduler

	q, err := s.lookupJobQueue(ctx, id)
	if err != nil {
		return err
	}
	return q.store.RetryDeadJob(ctx, id)
}

// Delete removes a terminal job while preserving its independent uniqueness TTL.
func (j Jobs) Delete(ctx context.Context, id string) error {
	if j.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := j.scheduler

	q, err := s.lookupJobQueue(ctx, id)
	if err != nil {
		return err
	}
	return q.store.DeleteJob(ctx, id)
}

func (j Jobs) RetryBatch(ctx context.Context, ids []string) []RetryResult {
	if j.scheduler == nil {
		results := make([]RetryResult, len(ids))
		for i, id := range ids {
			results[i] = RetryResult{ID: id, Err: ErrInvalidArgument}
		}
		return results
	}

	s := j.scheduler

	out := make([]RetryResult, len(ids))
	for i, id := range ids {
		out[i] = RetryResult{ID: id, Err: j.Retry(ctx, id)}
		if ctx.Err() != nil {
			for j := i + 1; j < len(ids); j++ {
				out[j] = RetryResult{ID: ids[j], Err: ctx.Err()}
			}
			break
		}
		if i+1 < len(ids) && s.config.ReplayInterval > 0 && !waitContext(ctx, s.config.ReplayInterval) {
			for j := i + 1; j < len(ids); j++ {
				out[j] = RetryResult{ID: ids[j], Err: ctx.Err()}
			}
			break
		}
	}
	return out
}

type RetryResult struct {
	ID  string
	Err error
}

type DeadQuery = model.DeadQuery

// Attempts returns bounded execution history newest first. Empty means disabled,
// expired, or no completed attempt; it does not imply that the Job exists.
func (j Jobs) Attempts(ctx context.Context, id string) ([]Attempt, error) {
	if j.scheduler == nil || id == "" {
		return nil, ErrInvalidArgument
	}
	return j.scheduler.namespace.Attempts(ctx, id)
}
