package scheduler

import "time"

type State string

const (
	StateScheduled State = "scheduled"
	StateReady     State = "ready"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateDead      State = "dead"
	StateCancelled State = "cancelled"
)

type Job struct {
	ID          string
	Type        string
	State       State
	Payload     []byte
	Attempt     int
	MaxAttempts int
	RunAt       time.Time
	CreatedAt   time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	LastError   string
	UniqueKey   string
	Definition  string
}

type EnqueueOption func(*enqueueOptions)
type enqueueOptions struct {
	runAt     time.Time
	delay     time.Duration
	uniqueKey string
	uniqueTTL time.Duration
}

func At(t time.Time) EnqueueOption {
	return func(o *enqueueOptions) { o.runAt, o.delay = t, 0 }
}
func Delay(d time.Duration) EnqueueOption {
	return func(o *enqueueOptions) { o.runAt, o.delay = time.Time{}, d }
}
func Unique(key string, ttl time.Duration) EnqueueOption {
	return func(o *enqueueOptions) { o.uniqueKey, o.uniqueTTL = key, ttl }
}
