package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func (s *Queue) PruneExpiredDeadIndex(ctx context.Context, batch int64) (int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := PruneExpiredDeadIndex.Run(ctx, s.Client, []string{s.Keys.Dead()}, batch).Int64()
	if err != nil {
		return 0, fmt.Errorf("cleanup expired dead jobs: %w", err)
	}
	return result, nil
}

func (s *Queue) PurgeExpiredSchedules(ctx context.Context, batch int64) (int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := PurgeExpiredSchedules.Run(
		ctx,
		s.Client,
		[]string{s.Keys.TerminalSchedules(), s.Keys.ScheduleCatalog(), s.Keys.Schedules()},
		s.Keys.SchedulePrefix(),
		batch,
	).
		Int64()
	if err != nil {
		return 0, fmt.Errorf("cleanup expired cron schedules: %w", err)
	}
	return result, nil
}

func (s *Queue) AcquireMaintenanceLease(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	return s.Client.SetNX(ctx, s.Keys.MaintenanceLease(), owner, ttl).Result()
}

func (s *Queue) RenewMaintenanceLease(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := MaintenanceRenew.Run(ctx, s.Client, []string{s.Keys.MaintenanceLease()}, owner, CeilDurationMillis(ttl)).Int64()
	return result == 1, err
}

func (s *Queue) ReleaseMaintenanceLease(ctx context.Context, owner string) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	_, err := MaintenanceRelease.Run(ctx, s.Client, []string{s.Keys.MaintenanceLease()}, owner).Result()
	return err
}

func (s *Queue) PruneOrphanedJobIndex(ctx context.Context, cursor uint64, batch int64) (uint64, int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	return s.pruneOrphanedIndex(ctx, s.Keys.Scheduled(), s.Keys.Job, cursor, batch)
}

func (s *Queue) PruneOrphanedScheduleIndex(ctx context.Context, cursor uint64, batch int64) (uint64, int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	return s.pruneOrphanedIndex(ctx, s.Keys.ScheduleCatalog(), s.Keys.Schedule, cursor, batch, s.Keys.Schedules())
}

func (s *Queue) pruneOrphanedIndex(ctx context.Context, index string, entityKey func(string) string, cursor uint64, batch int64, secondaryIndexes ...string) (uint64, int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	entries, next, err := s.Client.ZScan(ctx, index, cursor, "*", batch).Result()
	if err != nil {
		return cursor, 0, err
	}
	ids := make([]string, 0, len(entries)/2)
	pipe := s.Client.Pipeline()
	checks := make([]*redis.IntCmd, 0, len(entries)/2)
	for i := 0; i+1 < len(entries); i += 2 {
		ids = append(ids, entries[i])
		checks = append(checks, pipe.Exists(ctx, entityKey(entries[i])))
	}
	if len(checks) == 0 {
		return next, 0, nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return cursor, 0, err
	}
	orphans := make([]any, 0)
	for i, check := range checks {
		if check.Val() == 0 {
			orphans = append(orphans, ids[i])
		}
	}
	if len(orphans) == 0 {
		return next, 0, nil
	}
	removed, err := s.Client.ZRem(ctx, index, orphans...).Result()
	if err != nil {
		return cursor, 0, err
	}
	for _, secondary := range secondaryIndexes {
		if err := s.Client.ZRem(ctx, secondary, orphans...).Err(); err != nil {
			return cursor, removed, err
		}
	}
	return next, removed, nil
}

func (s *Queue) PruneStaleConsumers(ctx context.Context, idle time.Duration, limit int64) (int64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	consumers, err := s.Client.XInfoConsumers(ctx, s.Keys.Ready(), s.Keys.Group()).Result()
	if err != nil {
		if strings.Contains(err.Error(), "NOGROUP") || strings.Contains(err.Error(), "no such key") {
			return 0, nil
		}
		return 0, err
	}
	var removed int64
	for _, consumer := range consumers {
		if removed >= limit {
			break
		}
		if consumer.Pending != 0 || consumer.Idle < idle {
			continue
		}
		count, err := DeleteConsumer.Run(ctx, s.Client, []string{s.Keys.Ready()}, s.Keys.Group(), consumer.Name).Int64()
		if err != nil {
			return removed, err
		}
		removed += count
	}
	return removed, nil
}
