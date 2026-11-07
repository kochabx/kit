package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/kochabx/kit/core/scheduler/internal/store"
)

func (s *queueRuntime) receiveLoop(ctx context.Context, deliveries chan<- store.Delivery) error {
	backoff := newFailureBackoff(s.scheduler.config.FailureBackoff, s.scheduler.config.FailureBackoffMax)
	for {
		batch, err := s.store.ReadDeliveries(ctx, s.scheduler.readerConsumerName(), s.scheduler.config.Execution.PollTimeout, s.settings.FetchBatch)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Msg("receive failed")
			if !waitContext(ctx, backoff.NextDelay()) {
				return nil
			}
			continue
		}
		backoff.Reset()
		for i, d := range batch {
			select {
			case deliveries <- d:
			case <-ctx.Done():
				s.releaseUnstarted(ctx, batch[i:])
				return nil
			}
		}
	}
}

func (s *queueRuntime) executeLoop(ctx context.Context, worker string, deliveries <-chan store.Delivery) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case d := <-deliveries:
			if ctx.Err() != nil {
				s.releaseUnstarted(ctx, []store.Delivery{d})
				return nil
			}
			s.execute(ctx, worker, d)
		}
	}
}

func (s *queueRuntime) execute(parent context.Context, worker string, d store.Delivery) {
	token := uuid.NewString()
	j, err := s.store.ClaimJob(parent, d, store.ClaimCommand{
		Token:              token,
		Worker:             worker,
		Duration:           s.scheduler.config.Execution.LeaseDuration,
		ExpiredRetention:   s.scheduler.config.Retention.Expired,
		CancelledRetention: s.scheduler.config.Retention.Cancelled,
	})
	if err != nil {
		if !errors.Is(err, ErrInvalidState) && !errors.Is(err, ErrLeaseLost) && parent.Err() == nil {
			s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Str("job_id", d.JobID).Msg("start job failed")
		}
		return
	}
	if d.Recovered {
		s.scheduler.observe(EventRecovered, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt})
	}
	h, ok := s.scheduler.lookupHandler(s.settings.Name, j.TaskName, j.Version)
	if !ok {
		s.rejectJob(parent, d, j, token, ErrNoHandler)
		return
	}
	if h.queue != s.settings.Name || h.fingerprint != j.Fingerprint {
		s.rejectJob(parent, d, j, token, ErrTaskMismatch)
		return
	}
	retryPolicy, policyErr := retryPolicy(j.Policy, h)
	if policyErr != nil {
		s.rejectJob(parent, d, j, token, policyErr)
		return
	}
	s.scheduler.observe(EventStarted, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt})
	started := time.Now()
	executionCtx, stopExecution := context.WithCancelCause(context.WithoutCancel(parent))
	timeoutCtx, timeoutCancel := context.WithTimeout(executionCtx, j.Policy.Timeout)
	execCtx, cancel := context.WithCancelCause(timeoutCtx)
	defer stopExecution(nil)
	defer timeoutCancel()
	defer cancel(nil)
	execution := &activeExecution{queue: s.settings.Name, taskName: j.TaskName, token: token, ctx: execCtx, cancel: stopExecution,
		nextRenew: started.Add(s.scheduler.config.Execution.LeaseRenewInterval), nextCheck: started.Add(s.scheduler.config.Execution.CancellationCheckInterval)}
	s.scheduler.active.Store(j.ID, execution)
	defer s.scheduler.active.CompareAndDelete(j.ID, execution)
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result <- Permanent(fmt.Errorf("handler panic: %v\n%s", recovered, debug.Stack()))
			}
		}()
		result <- h.run(execCtx, j.Payload)
	}()
	var execErr error
	handlerReturned := false
	select {
	case execErr = <-result:
		handlerReturned = true
	case <-execCtx.Done():
		execErr = context.Cause(execCtx)
	}
	// Stop heartbeats before committing a terminal state. In-flight results refer
	// to this execution context/token and cannot cancel a subsequent execution.
	execution.finished.Store(true)
	if errors.Is(context.Cause(execCtx), ErrJobCancelled) {
		execErr = ErrJobCancelled
	}
	if cause := context.Cause(executionCtx); cause != nil {
		execErr = cause
	}
	if errors.Is(execErr, ErrLeaseLost) {
		s.scheduler.observe(EventLeaseLost, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt, Err: execErr})
	}
	duration := time.Since(started)
	opCtx, opCancel := s.scheduler.operationContext(parent)
	defer opCancel()
	if execErr == nil || errors.Is(execErr, ErrJobCancelled) {
		err = s.store.CompleteJob(opCtx, d, token, s.scheduler.config.Retention.Succeeded, s.scheduler.config.Retention.Cancelled)
	} else if !errors.Is(execErr, ErrLeaseLost) && !errors.Is(execErr, ErrShutdownTimeout) {
		delay, retry, policyErr := evaluateRetry(retryPolicy, j.FailureCount+1, j.MaxAttempts, execErr)
		if policyErr != nil {
			execErr = errors.Join(execErr, policyErr)
		}
		err = s.store.FailJob(opCtx, d, token, store.FailureCommand{
			Message:            execErr.Error(),
			Delay:              delay,
			Retry:              retry,
			DeadRetention:      s.scheduler.config.Retention.Dead,
			CancelledRetention: s.scheduler.config.Retention.Cancelled,
		})
		if err == nil && retry {
			s.scheduler.observe(EventRetried, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt, Err: execErr})
		}
	}
	if errors.Is(err, ErrLeaseLost) && !errors.Is(execErr, ErrLeaseLost) {
		s.scheduler.observe(EventLeaseLost, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt, Err: err})
	}
	if errors.Is(err, ErrJobCancelled) {
		execErr = ErrJobCancelled
		err = nil // The cancelled state was committed successfully.
	}
	if err != nil && !errors.Is(err, ErrLeaseLost) && !errors.Is(err, ErrJobCancelled) {
		s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Str("job_id", j.ID).Msg("finish job failed")
	}
	s.scheduler.observe(EventFinished, parent, Event{
		Queue:     s.settings.Name,
		JobID:     j.ID,
		TaskName:  j.TaskName,
		Attempt:   j.Attempt,
		Duration:  duration,
		Err:       execErr,
		CommitErr: err,
	})
	// A timed-out handler still occupies the real slot until it returns. Only
	// forced Worker shutdown may abandon this wait; Go cannot kill a goroutine.
	if !handlerReturned {
		select {
		case <-result:
		case <-s.scheduler.forceStop:
		}
	}
}

func (s *queueRuntime) releaseUnstarted(parent context.Context, batch []store.Delivery) {
	ctx, cancel := s.scheduler.operationContext(parent)
	defer cancel()
	if err := s.store.ReleaseDeliveries(ctx, batch); err != nil {
		s.scheduler.config.Observability.Logger.Warn().Err(err).Str("queue", s.settings.Name).Msg("release unstarted deliveries failed")
	}
}

// All pre-handler rejections report durable commit failures and the final event.
func (s *queueRuntime) rejectJob(parent context.Context, d store.Delivery, j *Job, token string, cause error) {
	ctx, cancel := s.scheduler.operationContext(parent)
	defer cancel()
	err := s.store.FailJob(ctx, d, token, store.FailureCommand{Message: cause.Error(), DeadRetention: s.scheduler.config.Retention.Dead, CancelledRetention: s.scheduler.config.Retention.Cancelled})
	if errors.Is(err, ErrJobCancelled) {
		cause = ErrJobCancelled
		err = nil
	}
	if errors.Is(err, ErrLeaseLost) {
		s.scheduler.observe(EventLeaseLost, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt, Err: err})
	}
	if err != nil {
		s.scheduler.config.Observability.Logger.Error().Err(err).Str("queue", s.settings.Name).Str("job_id", j.ID).Msg("commit rejected job failed")
	}
	s.scheduler.observe(EventFinished, parent, Event{Queue: s.settings.Name, JobID: j.ID, TaskName: j.TaskName, Attempt: j.Attempt, Err: cause, CommitErr: err})
}
