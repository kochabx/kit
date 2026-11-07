package scheduler

import (
	"fmt"
	"slices"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

type queueRuntime struct {
	scheduler *workerRuntime
	store     *store.Queue
	settings  QueueConfig
}

func (s *workerRuntime) lookupQueue(name string) (*queueRuntime, error) {
	q, ok := s.queues[name]
	if !ok {
		return nil, fmt.Errorf("queue %q: %w", name, ErrUnknownQueue)
	}
	return q, nil
}

func subscribesToQueue(name string, names []string) bool {
	if names == nil {
		return true
	}
	return slices.Contains(names, name)
}
