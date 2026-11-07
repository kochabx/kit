package store

import (
	"strconv"
	"time"
)

func decodeJob(id string, m map[string]string) *Job {
	j := &Job{
		Version: m["version"], Policy: decodePolicy(m["policy"]), RecoveryCount: parseInt(m["recovery_count"]), FailureCount: parseInt(m["failure_count"]),
		Queue:       m["queue"],
		ID:          id,
		TaskName:    m["task"],
		State:       JobState(m["state"]),
		Payload:     []byte(m["payload"]),
		LastError:   m["last_error"],
		UniqueKey:   m["unique_key"],
		Fingerprint: m["fingerprint"],
	}
	j.Attempt = parseInt(m["attempt"])
	j.MaxAttempts = parseInt(m["max_attempts"])
	j.RunAt = ParseTimeMillis(m["run_at"])
	j.CreatedAt = ParseTimeMillis(m["created_at"])
	j.StartedAt = ParseTimeMillis(m["started_at"])
	j.FinishedAt = ParseTimeMillis(m["finished_at"])
	j.ExpiresAt = ParseTimeMillis(m["expires_at"])
	return j
}

func decodeSchedule(id string, m map[string]string) *Schedule {
	return &Schedule{
		Queue:      m["queue"],
		ID:         id,
		TaskName:   m["task"],
		Expression: m["cron"],
		TimeZone:   m["timezone"],
		NextAt:     ParseTimeMillis(m["next_at"]),
		State:      ScheduleState(m["state"]),
		LastRunAt:  ParseTimeMillis(m["last_run_at"]),
	}
}

// timeMillis encodes an optional time; zero means no deadline in the Redis protocol.
func TimeMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func parseInt(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

func ParseTimeMillis(v string) time.Time {
	n, _ := strconv.ParseInt(v, 10, 64)
	if n == 0 {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

// ceilDurationMillis rounds positive durations up so a sub-millisecond TTL never becomes permanent.
func CeilDurationMillis(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return int64((value-1)/time.Millisecond + 1)
}
