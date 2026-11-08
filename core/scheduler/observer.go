package scheduler

import (
	"context"
	"time"
)

type EventKind string

const (
	EventEnqueued  EventKind = "enqueued"
	EventStarted   EventKind = "started"
	EventFinished  EventKind = "finished"
	EventRetried   EventKind = "retried"
	EventRecovered EventKind = "recovered"
	EventLeaseLost EventKind = "lease_lost"
)

type Event struct {
	Kind     EventKind
	Queue    string
	JobID    string
	TaskName string
	Attempt  int
	Duration time.Duration
	// Err describes handler execution or retry-policy failure.
	Err error
	// CommitErr describes failure to persist the execution result.
	// A nil Err does not imply success when CommitErr is non-nil.
	CommitErr error
}

// Observer receives asynchronous events with a timeout Context.
// Callbacks must honor cancellation; Go cannot stop an uncooperative callback.
type Observer interface{ Observe(context.Context, Event) }
type ObserverFunc func(context.Context, Event)

func (f ObserverFunc) Observe(ctx context.Context, event Event) {
	f(ctx, event)
}
