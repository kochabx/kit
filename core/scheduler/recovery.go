package scheduler

import (
	"context"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

func (s *queueRuntime) recoverLoop(ctx context.Context, deliveries chan<- store.Delivery) error {
	ticker := time.NewTicker(s.scheduler.config.Execution.LeaseDuration / 2)
	defer ticker.Stop()
	cursor := "0-0"
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		var err error
		cursor, err = s.recoverPending(ctx, deliveries, cursor)
		if err != nil && ctx.Err() == nil {
			s.scheduler.config.Observability.Logger.Error().Str("queue", s.settings.Name).Err(err).Msg("recover jobs failed")
		}
	}
}

// recoverPending limits both claimed messages and Redis calls. Empty pages can
// still advance the PEL cursor; unfinished scans resume on the next cycle.
func (s *queueRuntime) recoverPending(ctx context.Context, deliveries chan<- store.Delivery, cursor string) (string, error) {
	config := s.scheduler.config.Execution
	calls := (config.RecoveryLimit-1)/config.RecoveryBatch + 1
	recovered := int64(0)
	for range calls {
		count := min(config.RecoveryBatch, config.RecoveryLimit-recovered)
		messages, next, err := s.store.RecoverDeliveries(ctx, s.scheduler.reclaimerConsumerName(), config.LeaseDuration, cursor, count)
		if err != nil {
			return cursor, err
		}
		for i, message := range messages {
			if message.JobID == "" {
				continue
			}
			select {
			case deliveries <- message:
				recovered++
			case <-ctx.Done():
				s.releaseUnstarted(ctx, messages[i:])
				return cursor, ctx.Err()
			}
		}
		previous := cursor
		cursor = next
		if cursor == "0-0" || cursor == previous || recovered >= config.RecoveryLimit {
			break
		}
	}
	return cursor, nil
}
