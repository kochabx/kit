package scheduler

import (
	"context"
	"fmt"
)

type handlerKey struct{ queue, name, version string }

type handler struct {
	version       string
	run           func(context.Context, []byte) error
	retry         RetryPolicy
	retryIdentity string
	fingerprint   string
	queue         string
}

func (s *workerRuntime) registerHandler(queue, name string, h handler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return ErrClosed
	}
	if s.running.Load() {
		return fmt.Errorf("register %q: %w", name, ErrAlreadyRun)
	}
	if _, exists := s.handlers[handlerKey{queue, name, h.version}]; exists {
		return fmt.Errorf("%s/%s: %w", queue, name, ErrTaskRegistered)
	}
	s.handlers[handlerKey{queue, name, h.version}] = h
	return nil
}

func (s *workerRuntime) lookupHandler(queue, name string, versions ...string) (handler, bool) {
	version := "1"
	if len(versions) > 0 {
		version = versions[0]
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.handlers[handlerKey{queue, name, version}]
	return h, ok
}
