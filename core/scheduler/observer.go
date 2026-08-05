package scheduler

import (
	"context"
	"time"
)

type Event struct {
	JobID    string
	Type     string
	Attempt  int
	Duration time.Duration
	Err      error
}

type Observer interface {
	Enqueued(context.Context, Event)
	Started(context.Context, Event)
	Finished(context.Context, Event)
}

type noopObserver struct{}

func (noopObserver) Enqueued(context.Context, Event) {}
func (noopObserver) Started(context.Context, Event)  {}
func (noopObserver) Finished(context.Context, Event) {}
