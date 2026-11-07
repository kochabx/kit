package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
)

func (s *queueRuntime) maintenanceLoop(ctx context.Context) error {
	backoff := newFailureBackoff(s.scheduler.config.FailureBackoff, s.scheduler.config.FailureBackoffMax)
	var scheduledCursor, cronCursor uint64
	for {
		owner := s.scheduler.consumerID + ":" + uuid.NewString()
		acquired, err := s.store.AcquireMaintenanceLease(ctx, owner, s.scheduler.config.Maintenance.LeaseDuration)
		if err == nil && !acquired {
			if !waitContext(ctx, s.scheduler.config.Maintenance.Interval) {
				return nil
			}
			continue
		}
		var nextScheduled, nextCron uint64
		if err == nil {
			nextScheduled, nextCron, err = s.maintainWithLease(ctx, owner, scheduledCursor, cronCursor)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Msg("scheduler maintenance failed")
			if !waitContext(ctx, backoff.NextDelay()) {
				return nil
			}
			continue
		}
		scheduledCursor, cronCursor = nextScheduled, nextCron
		backoff.Reset()
		if !waitContext(ctx, s.scheduler.config.Maintenance.Interval) {
			return nil
		}
	}
}

func (s *queueRuntime) maintainWithLease(ctx context.Context, owner string, scheduledCursor, cronCursor uint64) (uint64, uint64, error) {
	leaseCtx, cancelLease := context.WithCancelCause(ctx)
	renewDone := make(chan error, 1)
	go func() {
		renewInterval := s.scheduler.config.Maintenance.LeaseDuration / 3
		if renewInterval <= 0 {
			renewInterval = s.scheduler.config.Maintenance.LeaseDuration
		}
		ticker := time.NewTicker(renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				renewDone <- nil
				return
			case <-ticker.C:
				ok, err := s.store.RenewMaintenanceLease(leaseCtx, owner, s.scheduler.config.Maintenance.LeaseDuration)
				if err == nil && !ok {
					err = ErrMaintenanceLeaseLost
				}
				if err != nil {
					cancelLease(err)
					renewDone <- err
					return
				}
			}
		}
	}()
	nextScheduled, nextCron, err := s.maintainQueue(leaseCtx, scheduledCursor, cronCursor)
	cancelLease(nil)
	if renewErr := <-renewDone; err == nil && renewErr != nil {
		err = renewErr
	}
	if ctx.Err() == nil {
		releaseCtx, releaseCancel := s.scheduler.operationContext(ctx)
		releaseErr := s.store.ReleaseMaintenanceLease(releaseCtx, owner)
		releaseCancel()
		if releaseErr != nil {
			s.scheduler.config.Observability.Logger.Warn().Str("queue", s.settings.Name).Err(releaseErr).Msg("release scheduler maintenance lease failed")
		}
	}
	return nextScheduled, nextCron, err
}

func (s *queueRuntime) maintainQueue(ctx context.Context, scheduledCursor, cronCursor uint64) (uint64, uint64, error) {
	for drained := int64(0); drained < s.scheduler.config.Maintenance.DrainLimit; {
		batch := min(s.scheduler.config.Maintenance.Batch, s.scheduler.config.Maintenance.DrainLimit-drained)
		removed, err := s.store.PruneExpiredDeadIndex(ctx, batch)
		if err != nil {
			return scheduledCursor, cronCursor, err
		}
		drained += removed
		if removed < batch {
			break
		}
	}
	for drained := int64(0); drained < s.scheduler.config.Maintenance.DrainLimit; {
		batch := min(s.scheduler.config.Maintenance.Batch, s.scheduler.config.Maintenance.DrainLimit-drained)
		removed, err := s.store.PurgeExpiredSchedules(ctx, batch)
		if err != nil {
			return scheduledCursor, cronCursor, err
		}
		drained += removed
		if removed < batch {
			break
		}
	}

	nextScheduled, _, err := s.store.PruneOrphanedJobIndex(ctx, scheduledCursor, s.scheduler.config.Maintenance.Batch)
	if err != nil {
		return scheduledCursor, cronCursor, err
	}
	nextCron, _, err := s.store.PruneOrphanedScheduleIndex(ctx, cronCursor, s.scheduler.config.Maintenance.Batch)
	if err != nil {
		return nextScheduled, cronCursor, err
	}
	if _, err := s.store.PruneStaleConsumers(ctx, s.scheduler.config.Maintenance.ConsumerIdleTimeout, s.scheduler.config.Maintenance.Batch); err != nil {
		return nextScheduled, nextCron, err
	}
	return nextScheduled, nextCron, nil
}
