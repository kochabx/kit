package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

type activeExecution struct {
	finished  atomic.Bool
	taskName  string
	queue     string
	token     string
	ctx       context.Context
	cancel    context.CancelCauseFunc
	nextRenew time.Time
	nextCheck time.Time
}

// heartbeatLoop owns nextRenew/nextCheck for every execution. It keeps running
// while Run drains handlers, independently of the intake context.
func (s *workerRuntime) heartbeatLoop(ctx context.Context) error {
	timer := time.NewTimer(min(s.config.Execution.LeaseRenewInterval, s.config.Execution.CancellationCheckInterval))
	defer timer.Stop()
	batch := heartbeatBatch{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			timer.Reset(batch.poll(s, ctx, time.Now()))
		}
	}
}

type heartbeatBatch struct {
	requests   []store.LeaseRequest
	executions []*activeExecution
}

func (b *heartbeatBatch) poll(s *workerRuntime, ctx context.Context, now time.Time) time.Duration {
	next := now.Add(min(s.config.Execution.LeaseRenewInterval, s.config.Execution.CancellationCheckInterval))
	requests := b.requests[:0]
	executions := b.executions[:0]
	defer func() {
		b.requests = requests[:0]
		clear(executions)
		b.executions = executions[:0]
	}()
	s.active.Range(func(key, value any) bool {
		execution := value.(*activeExecution)
		if execution.finished.Load() || execution.ctx.Err() != nil {
			return true
		}
		renew := !now.Before(execution.nextRenew)
		if !renew && now.Before(execution.nextCheck) {
			next = earliest(next, execution.nextRenew, execution.nextCheck)
			return true
		}
		requests = append(requests, store.LeaseRequest{JobID: key.(string), Token: execution.token, Lease: s.config.Execution.LeaseDuration, Renew: renew})
		executions = append(executions, execution)
		return true
	})
	if len(requests) == 0 {
		return max(time.Nanosecond, time.Until(next))
	}
	opCtx, cancel := context.WithTimeout(ctx, min(s.config.OperationTimeout, s.config.Execution.LeaseDuration/3))
	results := s.namespace.HeartbeatBatch(opCtx, requests)
	cancel()
	for i, result := range results {
		execution := executions[i]
		if execution.finished.Load() || execution.ctx.Err() != nil {
			continue
		}
		if result.Err == nil && !result.OK {
			result.Err = ErrLeaseLost
		}
		if result.Err != nil {
			if requests[i].Renew || errors.Is(result.Err, ErrLeaseLost) || errors.Is(result.Err, ErrJobCancelled) {
				execution.cancel(result.Err)
			} else {
				execution.nextCheck = now.Add(s.config.Execution.CancellationCheckInterval)
				next = earliest(next, execution.nextRenew, execution.nextCheck)
				s.config.Observability.Logger.Warn().Err(result.Err).Str("queue", execution.queue).Str("job_id", requests[i].JobID).Msg("cancellation check failed")
			}
			continue
		}
		// Schedule from request start, so network time never extends a lease locally.
		if requests[i].Renew {
			execution.nextRenew = now.Add(s.config.Execution.LeaseRenewInterval)
		}
		execution.nextCheck = now.Add(s.config.Execution.CancellationCheckInterval)
		next = earliest(next, execution.nextRenew, execution.nextCheck)
	}
	return max(time.Nanosecond, time.Until(next))
}

func earliest(times ...time.Time) time.Time {
	next := times[0]
	for _, candidate := range times[1:] {
		if candidate.Before(next) {
			next = candidate
		}
	}
	return next
}
