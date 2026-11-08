package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kochabx/kit/cx"
	"github.com/kochabx/kit/transport"
)

var (
	ErrInvalidOption  = errors.New("invalid application option")
	ErrAlreadyRunning = errors.New("application is already running")
	ErrAlreadyRun     = errors.New("application has already run")
)

// Application owns a single application lifecycle. Concurrent Run calls are rejected.
type Application struct {
	container       *cx.Container
	ctx             context.Context
	cancel          context.CancelFunc
	shutdownTimeout time.Duration
	signals         []os.Signal
	state           atomic.Uint32 // 0: new, 1: running, 2: finished
}

// New validates options and creates an application. It does not start components.
func New(options ...Option) (*Application, error) {
	b := &builder{ctx: context.Background(), shutdownTimeout: 30 * time.Second,
		signals: []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT}}
	for index, opt := range options {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := opt(b); err != nil {
			return nil, err
		}
	}
	if b.container != nil && (len(b.cxOpts) > 0 || len(b.components) > 0 || len(b.servers) > 0) {
		return nil, fmt.Errorf("%w: configure components, servers and hooks on the external container", ErrInvalidOption)
	}
	container := b.container
	if container != nil && container.State() != cx.StateNew {
		return nil, fmt.Errorf("%w: external container must be new", ErrInvalidOption)
	}
	if container == nil {
		b.cxOpts = append(b.cxOpts, cx.WithShutdownTimeout(b.shutdownTimeout))
		container = cx.New(b.cxOpts...)
		for _, component := range b.components {
			if err := component(container); err != nil {
				return nil, fmt.Errorf("%w: register component: %w", ErrInvalidOption, err)
			}
		}
		for index, server := range b.servers {
			key := cx.NewKey[transport.Server](fmt.Sprintf("app:server:%d", index))
			if err := container.Supply(key, server); err != nil {
				return nil, fmt.Errorf("%w: register server: %w", ErrInvalidOption, err)
			}
		}
	}
	ctx, cancel := context.WithCancel(b.ctx)
	return &Application{container: container, ctx: ctx, cancel: cancel,
		shutdownTimeout: b.shutdownTimeout, signals: b.signals}, nil
}

// Container exposes the container. Register dependencies before Run. Prefer
// WithComponent so infrastructure is registered before application servers.
func (app *Application) Container() *cx.Container {
	return app.container
}

func (app *Application) HealthCheck(ctx context.Context) (cx.HealthReport, error) {
	return app.container.HealthCheck(ctx)
}
