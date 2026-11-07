package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func (s *Queue) scanIndex(ctx context.Context, key string, scan Scan) ([]string, uint64, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	entries, next, err := s.Client.ZScan(ctx, key, scan.Cursor, "*", scan.Count).Result()
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(entries)/2)
	for i := 0; i+1 < len(entries); i += 2 {
		ids = append(ids, entries[i])
	}
	return ids, next, nil
}

func (s *Queue) DeadJobs(ctx context.Context, query DeadQuery) (Page[*Job], error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	return s.scanJobs(ctx, s.Keys.Dead(), Scan{Cursor: query.Cursor, Count: query.Count}, JobDead, query.TaskName)
}

func (s *Queue) RunningJobs(ctx context.Context, scan Scan) (Page[*Job], error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	return s.scanJobs(ctx, s.Keys.Running(), scan, JobRunning, "")
}

func (s *Queue) scanJobs(ctx context.Context, key string, scan Scan, state JobState, taskName string) (Page[*Job], error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	ids, next, err := s.scanIndex(ctx, key, scan)
	if err != nil {
		return Page[*Job]{}, err
	}
	jobs, err := s.getJobs(ctx, ids)
	if err != nil {
		return Page[*Job]{}, err
	}
	page := Page[*Job]{Items: make([]*Job, 0, len(jobs)), NextCursor: next}
	for _, job := range jobs {
		if job.Queue == s.Keys.Name && job.State == state && (taskName == "" || job.TaskName == taskName) {
			page.Items = append(page.Items, job)
		}
	}
	return page, nil
}

func (s *Queue) RetryDeadJob(ctx context.Context, id string) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := RetryDead.Run(ctx, s.Client, []string{s.Keys.Job(id), s.Keys.Scheduled(), s.Keys.Dead()}, id).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrInvalidState
	}
	return nil
}

func (s *Queue) DeleteJob(ctx context.Context, id string) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := DeleteJob.Run(ctx, s.Client, []string{s.Keys.Job(id), s.Keys.Dead(), s.Keys.Scheduled(), s.Keys.Running()}, id, s.Keys.Name).Int64()
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

func (s *Queue) statsKeys() []string {
	return []string{s.Keys.Scheduled(), s.Keys.Ready(), s.Keys.Running(), s.Keys.Dead(), s.Keys.ScheduleCatalog()}
}

func (s *Queue) enqueueStats(ctx context.Context, pipe redis.Pipeliner) *redis.Cmd {
	return StatsScript.Eval(ctx, pipe, s.statsKeys(), s.Keys.Group())
}

func (s *Queue) Stats(ctx context.Context) (Stats, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	values, err := StatsScript.Run(ctx, s.Client, s.statsKeys(), s.Keys.Group()).Int64Slice()
	if err != nil {
		return Stats{}, err
	}
	return decodeStats(values)
}

func decodeStats(values []int64) (Stats, error) {
	if len(values) != 8 {
		return Stats{}, fmt.Errorf("scheduler stats: unexpected redis response")
	}
	return Stats{Scheduled: values[0], Ready: values[1], Pending: values[2],
		Running: values[3], Dead: values[4], Schedules: values[5],
		DispatchLag:     time.Duration(values[6]) * time.Millisecond,
		PendingHeadIdle: time.Duration(values[7]) * time.Millisecond}, nil
}

func (s *Namespace) StatsBatch(ctx context.Context, queues []*Queue) (Stats, error) {
	ctx, cancel := s.commandContext(ctx)
	defer cancel()
	var total Stats
	pipe := s.Client.Pipeline()
	commands := make([]*redis.Cmd, len(queues))
	for i, q := range queues {
		commands[i] = q.enqueueStats(ctx, pipe)
	}
	_, _ = pipe.Exec(ctx)
	for _, cmd := range commands {
		values, err := cmd.Int64Slice()
		if err != nil {
			return Stats{}, err
		}
		stats, err := decodeStats(values)
		if err != nil {
			return Stats{}, err
		}
		total.Scheduled += stats.Scheduled
		total.Ready += stats.Ready
		total.Pending += stats.Pending
		total.Running += stats.Running
		total.Dead += stats.Dead
		total.Schedules += stats.Schedules
		total.DispatchLag = max(total.DispatchLag, stats.DispatchLag)
		total.PendingHeadIdle = max(total.PendingHeadIdle, stats.PendingHeadIdle)
	}
	return total, nil
}
