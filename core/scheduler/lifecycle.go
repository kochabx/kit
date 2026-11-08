package scheduler

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

const (
	readerConsumerSuffix    = ":reader"
	reclaimerConsumerSuffix = ":reclaimer"
)

func (s *workerRuntime) readerConsumerName() string {
	return s.consumerID + readerConsumerSuffix
}

func (s *workerRuntime) reclaimerConsumerName() string {
	return s.consumerID + reclaimerConsumerSuffix
}

// Run dispatches and executes jobs until cancellation, then drains active work.
// A engine can run once; producer-only instances do not need Run.
func (s *workerRuntime) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return ErrClosed
	}
	if !s.running.CompareAndSwap(false, true) {
		s.mu.Unlock()
		return ErrAlreadyRun
	}
	s.mu.Unlock()
	defer func() { s.closed.Store(true); s.events.close() }()
	if err := s.namespace.CheckCapabilities(ctx); err != nil {
		return err
	}
	for _, q := range s.workers {
		if err := q.store.EnsureConsumerGroup(ctx); err != nil {
			return err
		}
	}
	s.config.Observability.Logger.Info().Str("consumer_id", s.consumerID).Int("queues", len(s.queueOrder)).Msg("scheduler started")

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	g, gctx := errgroup.WithContext(runCtx)
	leaseCtx, stopLease := context.WithCancel(context.WithoutCancel(ctx))
	defer stopLease()
	var leaseDone chan error
	if len(s.workers) > 0 {
		leaseDone = make(chan error, 1)
		go func() { leaseDone <- s.heartbeatLoop(leaseCtx) }()
	}
	for _, q := range s.dispatchers {
		g.Go(func() error { return q.dispatchLoop(gctx) })
		g.Go(func() error { return q.maintenanceLoop(gctx) })
	}
	pending := make(map[*queueRuntime]chan store.Delivery)
	for _, q := range s.workers {
		deliveries := make(chan store.Delivery, q.settings.Prefetch)
		pending[q] = deliveries
		g.Go(func() error { return q.receiveLoop(gctx, deliveries) })
		g.Go(func() error { return q.recoverLoop(gctx, deliveries) })
		for i := range q.settings.Concurrency {
			slot := fmt.Sprintf("%s:%s-%d", s.consumerID, q.settings.Name, i)
			g.Go(func() error { return q.executeLoop(gctx, slot, deliveries) })
		}
	}

	done := make(chan error, 1)
	go func() { done <- g.Wait() }()
	var err error
	select {
	case err = <-done:
		cancel()
	case <-ctx.Done():
		cancel() // stop intake; active executions keep their independent context
		timer := time.NewTimer(s.config.Execution.ShutdownTimeout)
		select {
		case err = <-done:
		case <-timer.C:
			err = ErrShutdownTimeout
			close(s.forceStop)
			s.active.Range(func(key, value any) bool {
				jobID, _ := key.(string)
				execution, _ := value.(*activeExecution)
				s.config.Observability.Logger.Error().
					Str("queue", execution.queue).
					Str("job_id", jobID).
					Str("task", execution.taskName).
					Msg("job still running at shutdown")
				execution.cancel(ErrShutdownTimeout)
				return true
			})
			force := time.NewTimer(s.config.OperationTimeout)
			select {
			case <-done:
			case <-force.C:
			}
			force.Stop()
		}
		timer.Stop()
	}
	stopLease()
	if leaseDone != nil {
		<-leaseDone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		if !errors.Is(err, ErrShutdownTimeout) {
			err = nil
		}
	}
	releaseCtx, cancelRelease := s.operationContext(ctx)
	defer cancelRelease()
	for q, ch := range pending {
		batch := make([]store.Delivery, 0, len(ch))
		for {
			select {
			case d := <-ch:
				batch = append(batch, d)
			default:
				if releaseErr := q.store.ReleaseDeliveries(releaseCtx, batch); releaseErr != nil {
					s.config.Observability.Logger.Warn().Err(releaseErr).Str("queue", q.settings.Name).Msg("release buffered deliveries failed")
				}
				goto drained
			}
		}
	drained:
	}
	s.config.Observability.Logger.Info().Msg("scheduler stopped")
	return err
}

func waitContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (s *workerRuntime) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), s.config.OperationTimeout)
}

// Close releases resources owned by a producer-only engine.
// The borrowed Redis client remains open.
func (s *workerRuntime) Close() error {
	s.mu.Lock()
	if s.running.Load() {
		s.mu.Unlock()
		return ErrAlreadyRun
	}
	if s.closed.Swap(true) {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	s.events.close()
	return nil
}
