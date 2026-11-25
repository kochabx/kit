// Package app provides bounded process lifecycle management around *cx.Container.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kochabx/kit/cx"
)

var (
	ErrInvalidOption  = errors.New("invalid application option")
	ErrAlreadyRunning = errors.New("application is already running")
	ErrAlreadyRun     = errors.New("application has already run")
)

// Server defines a managed long-running service started and stopped by Application.
type Server interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// Application owns a single process lifecycle wrapping *cx.Container.
type Application struct {
	container       *cx.Container
	logger          *slog.Logger
	shutdownTimeout time.Duration
	signals         []os.Signal

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped atomic.Bool
	state   atomic.Uint32 // 0: new, 1: running, 2: finished
}

// New validates options and builds an Application without starting components.
func New(options ...Option) (*Application, error) {
	b := &builder{
		shutdownTimeout: 30 * time.Second,
		signals:         []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT},
		logger:          slog.New(slog.DiscardHandler),
	}
	for index, opt := range options {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := opt(b); err != nil {
			return nil, err
		}
	}
	if b.container != nil && (len(b.cxOpts) > 0 || len(b.components) > 0 || len(b.servers) > 0 || len(b.runnables) > 0) {
		return nil, fmt.Errorf("%w: configure components, servers and hooks on the external container", ErrInvalidOption)
	}
	container := b.container
	if container != nil && container.State() != cx.StateNew {
		return nil, fmt.Errorf("%w: external container must be new", ErrInvalidOption)
	}
	if container == nil {
		b.cxOpts = append(b.cxOpts,
			cx.WithShutdownTimeout(b.shutdownTimeout),
			cx.WithComponentStopTimeout(min(b.shutdownTimeout, 10*time.Second)),
			cx.WithRollbackTimeout(b.shutdownTimeout),
		)
		container = cx.New(b.cxOpts...)
		for _, component := range b.components {
			if err := component(container); err != nil {
				return nil, fmt.Errorf("%w: register component: %w", ErrInvalidOption, err)
			}
		}
		for index, server := range b.servers {
			key := cx.NewKey[Server](fmt.Sprintf("app:server:%d", index))
			if err := container.Supply(key, server); err != nil {
				return nil, fmt.Errorf("%w: register server: %w", ErrInvalidOption, err)
			}
		}
		for index, runnable := range b.runnables {
			runner, err := cx.NewRunner(runnable)
			if err != nil {
				return nil, fmt.Errorf("%w: create runnable: %w", ErrInvalidOption, err)
			}
			key := cx.NewKey[*cx.Runner](fmt.Sprintf("app:runnable:%d", index))
			if err := container.Supply(key, runner); err != nil {
				return nil, fmt.Errorf("%w: register runnable: %w", ErrInvalidOption, err)
			}
		}
	}
	return &Application{
		container:       container,
		logger:          b.logger,
		shutdownTimeout: b.shutdownTimeout,
		signals:         b.signals,
	}, nil
}

// Container returns the backing *cx.Container.
func (a *Application) Container() *cx.Container { return a.container }

// HealthCheck probes all HealthChecker components registered on the container.
func (a *Application) HealthCheck(ctx context.Context) (cx.HealthReport, error) {
	return a.container.HealthCheck(ctx)
}

// Run starts components and blocks until ctx cancellation, OS signal, Shutdown,
// or supervised failure, then performs a bounded shutdown.
func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		return cx.ErrNilContext
	}
	if !a.state.CompareAndSwap(0, 1) {
		if a.state.Load() == 1 {
			return ErrAlreadyRunning
		}
		return ErrAlreadyRun
	}
	defer a.state.Store(2)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.mu.Lock()
	a.cancel = cancel
	stoppedEarly := a.stopped.Load()
	if stoppedEarly {
		cancel()
	}
	a.mu.Unlock()
	if stoppedEarly {
		return nil
	}

	if len(a.signals) > 0 {
		signalCtx, stopSignals := signal.NotifyContext(runCtx, a.signals...)
		defer stopSignals()
		runCtx = signalCtx
	}

	if err := a.container.Start(runCtx); err != nil {
		return fmt.Errorf("app: start: %w", err)
	}

	var runErr error
	if err := a.container.Wait(runCtx); err != nil && runCtx.Err() == nil {
		runErr = fmt.Errorf("app: supervised component: %w", err)
	}
	cancel()

	return errors.Join(runErr, a.shutdown(ctx))
}

// Shutdown requests application cancellation.
func (a *Application) Shutdown() {
	a.stopped.Store(true)
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Application) shutdown(parent context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), a.shutdownTimeout)
	defer cancel()
	if err := a.container.Stop(ctx); err != nil {
		a.logger.ErrorContext(ctx, "shutdown completed with errors", slog.Any("error", err))
		return fmt.Errorf("app: shutdown: %w", err)
	}
	return nil
}
