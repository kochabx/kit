package scheduler

import (
	"context"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

func (s *queueRuntime) dispatchLoop(ctx context.Context) error {
	ticker := time.NewTicker(s.scheduler.config.Dispatch.Interval)
	defer ticker.Stop()
	backoff := newFailureBackoff(s.scheduler.config.FailureBackoff, s.scheduler.config.FailureBackoffMax)
	for {
		var cycleErr error
		for drained := int64(0); drained < s.scheduler.config.Dispatch.DrainLimit; {
			batch := min(s.scheduler.config.Dispatch.Batch, s.scheduler.config.Dispatch.DrainLimit-drained)
			moved, err := s.store.DispatchJobs(ctx, batch, s.scheduler.config.Retention.Expired)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Msg("dispatch failed")
				cycleErr = err
				break
			}
			drained += moved
			if moved < batch {
				break
			}
		}
		if err := s.dispatchSchedules(ctx); err != nil && ctx.Err() == nil {
			s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Msg("cron dispatch failed")
			cycleErr = err
		}
		if cycleErr != nil {
			if !waitContext(ctx, backoff.NextDelay()) {
				return nil
			}
			continue
		}
		backoff.Reset()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *queueRuntime) dispatchSchedules(ctx context.Context) error {
	for drained := int64(0); drained < s.scheduler.config.Dispatch.CronDrainLimit; {
		batch := min(s.scheduler.config.Dispatch.Batch, s.scheduler.config.Dispatch.CronDrainLimit-drained)
		due, err := s.store.DueSchedules(ctx, batch)
		if err != nil {
			return err
		}
		fires := make([]store.ScheduleFire, 0, len(due.Records))
		for _, record := range due.Records {
			if record.State != ScheduleActive {
				if disableErr := s.store.InvalidateSchedule(ctx, record, "schedule is not active", s.scheduler.config.Retention.Schedule); disableErr != nil {
					return disableErr
				}
				continue
			}
			next, nextErr := s.scheduler.cronCache.nextRunMillis(record, due.Now)
			if nextErr != nil {
				s.scheduler.config.Observability.Logger.Error().
					Str("queue", s.settings.Name).
					Err(nextErr).
					Str("schedule_id", record.ID).
					Msg("invalid persisted cron schedule disabled")
				if disableErr := s.store.InvalidateSchedule(ctx, record, nextErr.Error(), s.scheduler.config.Retention.Schedule); disableErr != nil {
					return disableErr
				}
				continue
			}
			fires = append(fires, store.ScheduleFire{Record: record, Next: next})
		}
		if err := s.store.FireSchedules(ctx, fires); err != nil {
			return err
		}
		drained += due.Scanned
		if due.Scanned < batch {
			break
		}
	}
	return nil
}
