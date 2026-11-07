package scheduler

import (
	"context"
	"fmt"
	"time"
)

// Schedules updates recurrence without changing its immutable queue.
type Schedules struct{ scheduler *clientState }

func (s *clientState) Schedules() Schedules { return Schedules{scheduler: s} }

func (c Schedules) Get(ctx context.Context, id string) (*Schedule, error) {
	if c.scheduler == nil || id == "" {
		return nil, ErrInvalidArgument
	}

	s := c.scheduler

	return s.namespace.GetSchedule(ctx, id)
}

func (c Schedules) Cancel(ctx context.Context, id string) error {
	if c.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := c.scheduler

	q, err := s.lookupScheduleQueue(ctx, id)
	if err != nil {
		return err
	}

	return q.store.CancelSchedule(ctx, id, s.config.Retention.Schedule)
}

func (c Schedules) Pause(ctx context.Context, id string) error {
	if c.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := c.scheduler

	q, err := s.lookupScheduleQueue(ctx, id)
	if err != nil {
		return err
	}

	return q.store.SetScheduleState(ctx, id, false, time.Time{})
}

func (c Schedules) Resume(ctx context.Context, id string) error {
	if c.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := c.scheduler

	record, err := s.namespace.GetScheduleRecord(ctx, id)
	if err != nil {
		return err
	}
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil {
		return err
	}
	nextAt, err := s.cronCache.nextRunMillis(record, now)
	if err != nil {
		return err
	}
	if !validName.MatchString(record.Queue) {
		return fmt.Errorf("invalid persisted queue: %w", ErrInvalidState)
	}
	q, err := s.lookupQueue(record.Queue)
	if err != nil {
		return err
	}
	return q.store.SetScheduleState(ctx, id, true, time.UnixMilli(nextAt))
}

func (c Schedules) Delete(ctx context.Context, id string) error {
	if c.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	s := c.scheduler

	q, err := s.lookupScheduleQueue(ctx, id)
	if err != nil {
		return err
	}

	return q.store.DeleteSchedule(ctx, id)
}

// UpdateCron changes recurrence and time zone while preserving a paused schedule.
func (c Schedules) UpdateCron(ctx context.Context, id string, spec CronSpec) error {
	if c.scheduler == nil || id == "" {
		return ErrInvalidArgument
	}

	expression := spec.Expression

	s := c.scheduler

	q, err := s.lookupScheduleQueue(ctx, id)
	if err != nil {
		return err
	}

	parsed, loadedLocation, err := parseCron(spec)
	if err != nil {
		return err
	}
	now, err := s.namespace.ServerTimeMillis(ctx)
	if err != nil {
		return fmt.Errorf("read redis time: %w", err)
	}
	nextMillis, err := nextRunMillis(parsed, loadedLocation, now)
	if err != nil {
		return err
	}
	next := time.UnixMilli(nextMillis)
	return q.store.UpdateSchedule(ctx, id, expression, loadedLocation.String(), next)
}
