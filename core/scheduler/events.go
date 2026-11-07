package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kochabx/kit/log"
)

type observedEvent struct {
	ctx   context.Context
	event Event
}

// eventBus owns its goroutine and serializes publish against close. Callback
// panic recovery is an extension boundary, not a substitute for lifecycle locks.
type eventBus struct {
	mu              sync.RWMutex
	closed          bool
	queue           chan observedEvent
	done            chan struct{}
	dropped         atomic.Uint64
	observer        Observer
	logger          *log.Logger
	timeout         time.Duration
	callbackTimeout time.Duration
}

func newEventBus(config ObservabilityConfig, timeout time.Duration) *eventBus {
	if config.Observer == nil {
		return nil
	}
	bus := &eventBus{
		queue:           make(chan observedEvent, config.Buffer),
		done:            make(chan struct{}),
		observer:        config.Observer,
		logger:          config.Logger,
		timeout:         timeout,
		callbackTimeout: config.ObserverTimeout,
	}
	go bus.run()
	return bus
}

func (b *eventBus) publish(ctx context.Context, event Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		b.dropped.Add(1)
		return
	}
	select {
	case b.queue <- observedEvent{ctx: context.WithoutCancel(ctx), event: event}:
	default:
		b.dropped.Add(1)
	}
}

func (b *eventBus) run() {
	defer close(b.done)
	for item := range b.queue {
		func() {
			defer func() {
				if value := recover(); value != nil {
					b.dropped.Add(1)
					b.logger.Error().Interface("panic", value).Str("queue", item.event.Queue).Str("job_id", item.event.JobID).Msg("scheduler observer panicked")
				}
			}()
			timeout := b.callbackTimeout
			if timeout <= 0 {
				timeout = b.timeout
			}
			ctx, cancel := context.WithTimeout(item.ctx, timeout)
			defer cancel()
			b.observer.Observe(ctx, item.event)
		}()
	}
}

func (b *eventBus) close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.queue)
	}
	b.mu.Unlock()
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case <-b.done:
	case <-timer.C:
		b.logger.Error().Msg("scheduler observer flush timed out")
	}
}

func (b *eventBus) droppedCount() uint64 {
	if b == nil {
		return 0
	}
	return b.dropped.Load()
}

func (s *clientState) observe(kind EventKind, ctx context.Context, event Event) {
	event.Kind = kind
	s.events.publish(ctx, event)
}
