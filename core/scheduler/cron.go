package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type ScheduleState string

const (
	ScheduleStateActive    ScheduleState = "active"
	ScheduleStatePaused    ScheduleState = "paused"
	ScheduleStateCancelled ScheduleState = "cancelled"
	ScheduleStateInvalid   ScheduleState = "invalid"
)

type CronSchedule struct {
	ID, Type, Expression, Location string
	NextAt                         time.Time
	State                          ScheduleState
	LastRunAt                      time.Time
}

type ScheduleQuery struct {
	Offset, Limit int64
	State         ScheduleState
}

func ScheduleCron[T any](s *Scheduler, ctx context.Context, d Definition[T], payload T, expression string, location *time.Location) (*CronSchedule, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}
	if location == nil {
		location = time.UTC
	}
	if _, err := time.LoadLocation(location.String()); err != nil {
		return nil, fmt.Errorf("cron location must be UTC, Local, or an IANA location: %w", err)
	}
	if !validName.MatchString(d.name) || d.codec == nil || d.retry == nil || d.timeout <= 0 {
		return nil, fmt.Errorf("invalid definition %q", d.name)
	}
	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, fmt.Errorf("parse cron expression: %w", err)
	}
	b, err := d.codec.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	if len(b) > s.config.MaxPayloadBytes {
		return nil, fmt.Errorf("payload is %d bytes; limit is %d", len(b), s.config.MaxPayloadBytes)
	}
	fingerprint, err := definitionFingerprint(d)
	if err != nil {
		return nil, fmt.Errorf("fingerprint definition %q: %w", d.name, err)
	}
	now, err := s.store.timestamp(ctx)
	if err != nil {
		return nil, fmt.Errorf("read redis time: %w", err)
	}
	next := time.UnixMilli(nextCronTimestamp(parsed, location, now))
	schedule := CronSchedule{ID: uuid.NewString(), Type: d.name, Expression: expression, Location: location.String(), NextAt: next, State: ScheduleStateActive}
	if err := s.store.createSchedule(ctx, schedule, b, retryLimit(d.retry), fingerprint); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (s *Scheduler) CancelSchedule(ctx context.Context, id string) error {
	return s.store.cancelSchedule(ctx, id, s.config.ScheduleRetention)
}

func (s *Scheduler) GetSchedule(ctx context.Context, id string) (*CronSchedule, error) {
	return s.store.getSchedule(ctx, id)
}
func (s *Scheduler) ListSchedules(ctx context.Context, query ScheduleQuery) ([]*CronSchedule, error) {
	if query.Offset < 0 || query.Limit <= 0 || query.Limit > 1000 || (query.State != "" && query.State != ScheduleStateActive && query.State != ScheduleStatePaused && query.State != ScheduleStateCancelled && query.State != ScheduleStateInvalid) {
		return nil, ErrInvalidArgument
	}
	return s.store.listSchedules(ctx, query)
}
func (s *Scheduler) PauseSchedule(ctx context.Context, id string) error {
	return s.store.setScheduleState(ctx, id, false, time.Time{})
}
func (s *Scheduler) ResumeSchedule(ctx context.Context, id string) error {
	record, err := s.store.getScheduleRecord(ctx, id)
	if err != nil {
		return err
	}
	now, err := s.store.timestamp(ctx)
	if err != nil {
		return err
	}
	nextAt, err := nextCronFrom(record, now)
	if err != nil {
		return err
	}
	return s.store.setScheduleState(ctx, id, true, time.UnixMilli(nextAt))
}
func (s *Scheduler) DeleteSchedule(ctx context.Context, id string) error {
	return s.store.deleteSchedule(ctx, id)
}
func (s *Scheduler) UpdateSchedule(ctx context.Context, id, expression string, location *time.Location) error {
	if location == nil {
		location = time.UTC
	}
	loadedLocation, err := time.LoadLocation(location.String())
	if err != nil {
		return fmt.Errorf("cron location must be UTC, Local, or an IANA location: %w", err)
	}
	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return err
	}
	now, err := s.store.timestamp(ctx)
	if err != nil {
		return fmt.Errorf("read redis time: %w", err)
	}
	next := time.UnixMilli(nextCronTimestamp(parsed, loadedLocation, now))
	return s.store.updateSchedule(ctx, id, expression, location.String(), next)
}

func nextCronFrom(record scheduleRecord, from int64) (int64, error) {
	location, err := time.LoadLocation(record.Location)
	if err != nil {
		return 0, err
	}
	parsed, err := cron.ParseStandard(record.Expression)
	if err != nil {
		return 0, err
	}
	return nextCronTimestamp(parsed, location, from), nil
}

func nextCronTimestamp(schedule cron.Schedule, location *time.Location, from int64) int64 {
	return schedule.Next(time.UnixMilli(from).In(location)).UnixMilli()
}
