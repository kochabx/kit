package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

func (s *Queue) CreateSchedule(ctx context.Context, schedule Schedule, payload []byte, maxAttempts int, fingerprint string, version string, policy ExecutionPolicy) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	if err := s.CheckCapabilities(ctx); err != nil {
		return err
	}
	result, err := CreateSchedule.Run(
		ctx,
		s.Client,
		[]string{s.Keys.Schedule(schedule.ID), s.Keys.Schedules(), s.Keys.ScheduleCatalog()},
		schedule.ID,
		schedule.TaskName,
		payload,
		schedule.Expression,
		schedule.TimeZone,
		schedule.NextAt.UnixMilli(),
		maxAttempts,
		fingerprint,
		s.Keys.Name,
		version,
		encodePolicy(policy),
	).
		Int64()
	if err != nil {
		return fmt.Errorf("create cron schedule: %w", err)
	}
	if result == 0 {
		return ErrDuplicate
	}
	return nil
}

type ScheduleRecord struct{ Schedule }

type ScheduleBatch struct {
	Records []ScheduleRecord
	Now     int64
	Scanned int64
}

// dueSchedules returns bounded metadata and the Redis clock used to select it.
// Payload stays in Redis until fireSchedules creates the jobs.
func (s *Queue) DueSchedules(ctx context.Context, limit int64) (ScheduleBatch, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	values, err := DueSchedules.Run(ctx, s.Client, []string{s.Keys.Schedules()}, limit, s.Keys.SchedulePrefix(), s.Keys.Name).Slice()
	if err != nil {
		return ScheduleBatch{}, err
	}
	if len(values) < 2 {
		return ScheduleBatch{}, fmt.Errorf("invalid due schedules response")
	}
	now, ok := values[0].(int64)
	if !ok {
		return ScheduleBatch{}, fmt.Errorf("invalid due schedules clock")
	}
	scanned, ok := values[1].(int64)
	if !ok {
		return ScheduleBatch{}, fmt.Errorf("invalid due schedules count")
	}
	records := make([]ScheduleRecord, 0, len(values)-2)
	for _, value := range values[2:] {
		fields, ok := value.([]any)
		if !ok || len(fields) != 6 {
			return ScheduleBatch{}, fmt.Errorf("invalid due schedule record")
		}
		var strings [6]string
		for i, field := range fields {
			var ok bool
			strings[i], ok = field.(string)
			if !ok {
				return ScheduleBatch{}, fmt.Errorf("invalid due schedule field")
			}
		}
		records = append(records, ScheduleRecord{
			Queue:      s.Keys.Name,
			ID:         strings[0],
			Expression: strings[1],
			TimeZone:   strings[2],
			NextAt:     ParseTimeMillis(strings[3]),
			State:      ScheduleState(strings[4]),
			LastRunAt:  ParseTimeMillis(strings[5])})
	}
	return ScheduleBatch{Records: records, Now: now, Scanned: scanned}, nil
}

func (s *Queue) InvalidateSchedule(ctx context.Context, schedule ScheduleRecord, reason string, retention time.Duration) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	_, err := DisableSchedule.Run(
		ctx,
		s.Client,
		[]string{s.Keys.Schedule(schedule.ID), s.Keys.Schedules(), s.Keys.TerminalSchedules()},
		schedule.ID,
		schedule.NextAt.UnixMilli(),
		reason,
		CeilDurationMillis(retention),
	).
		Result()
	if err != nil {
		return fmt.Errorf("disable invalid cron schedule: %w", err)
	}
	return nil
}

type ScheduleFire struct {
	Record ScheduleRecord
	Next   int64
}

func (s *Queue) FireSchedules(ctx context.Context, records []ScheduleFire) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	if len(records) == 0 {
		return nil
	}
	// EVAL deliberately avoids NOSCRIPT partial pipeline failures after Redis restart.
	pipe := s.Client.Pipeline()
	for _, item := range records {
		schedule := item.Record
		instanceID := schedule.ID + ":" + strconv.FormatInt(schedule.NextAt.UnixMilli(), 10)
		FireSchedule.Eval(
			ctx,
			pipe,
			[]string{s.Keys.Schedule(schedule.ID), s.Keys.Schedules(), s.Keys.Job(instanceID), s.Keys.Ready()},
			schedule.NextAt.UnixMilli(),
			item.Next,
			instanceID,
			s.Keys.Name,
		)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Queue) CancelSchedule(ctx context.Context, id string, retention time.Duration) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := CancelSchedule.Run(
		ctx,
		s.Client,
		[]string{s.Keys.Schedule(id), s.Keys.Schedules(), s.Keys.TerminalSchedules()},
		id,
		CeilDurationMillis(retention),
	).
		Int64()
	if err != nil {
		return err
	}
	if result == -1 {
		return ErrNotFound
	}
	return nil
}

func (s *Queue) ListSchedules(ctx context.Context, query ScheduleQuery) (Page[*Schedule], error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	ids, next, err := s.scanIndex(ctx, s.Keys.ScheduleCatalog(), Scan{Cursor: query.Cursor, Count: query.Count})
	if err != nil {
		return Page[*Schedule]{}, err
	}
	page := Page[*Schedule]{Items: make([]*Schedule, 0, len(ids)), NextCursor: next}
	if len(ids) == 0 {
		return page, nil
	}
	pipe := s.Client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, s.Keys.Schedule(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return Page[*Schedule]{}, err
	}
	for i, cmd := range cmds {
		fields := cmd.Val()
		if len(fields) == 0 || fields["queue"] != s.Keys.Name || (query.State != "" && ScheduleState(fields["state"]) != query.State) {
			continue
		}
		page.Items = append(page.Items, decodeSchedule(ids[i], fields))
	}
	return page, nil
}

func (s *Queue) SetScheduleState(ctx context.Context, id string, active bool, next time.Time) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	state := SchedulePaused
	score := int64(0)
	if active {
		state = ScheduleActive
		score = next.UnixMilli()
	}
	result, err := SetScheduleState.Run(ctx, s.Client, []string{s.Keys.Schedule(id), s.Keys.Schedules(), s.Keys.TerminalSchedules()}, id, string(state), score).
		Int64()
	if err != nil {
		return err
	}
	if result < 0 {
		return ErrInvalidState
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Queue) DeleteSchedule(ctx context.Context, id string) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := DeleteSchedule.Run(
		ctx,
		s.Client,
		[]string{s.Keys.Schedule(id), s.Keys.Schedules(), s.Keys.ScheduleCatalog(), s.Keys.TerminalSchedules()},
		id,
	).
		Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Queue) UpdateSchedule(ctx context.Context, id, expression, location string, next time.Time) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := UpdateSchedule.Run(
		ctx,
		s.Client,
		[]string{s.Keys.Schedule(id), s.Keys.Schedules(), s.Keys.TerminalSchedules()},
		id,
		expression,
		location,
		next.UnixMilli(),
	).
		Int64()
	if err != nil {
		return err
	}
	if result < 0 {
		return ErrInvalidState
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}
