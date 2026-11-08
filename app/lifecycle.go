package app

import (
	"context"
	"errors"
	"fmt"
	"os/signal"

	"github.com/kochabx/kit/log"
)

// Run starts components and blocks until cancellation, a signal, or a supervised
// component failure. Each application may run only once, even after startup failure.
func (app *Application) Run() error {
	if !app.state.CompareAndSwap(0, 1) {
		if app.state.Load() == 1 {
			return ErrAlreadyRunning
		}
		return ErrAlreadyRun
	}
	defer app.state.Store(2)
	defer app.cancel()

	// Listen before starting so startup also observes process shutdown signals.
	ctx := app.ctx
	if len(app.signals) > 0 {
		signalCtx, stop := signal.NotifyContext(ctx, app.signals...)
		defer stop()
		ctx = signalCtx
	}
	if err := app.container.Start(ctx); err != nil {
		return fmt.Errorf("app: start: %w", err)
	}

	var runErr error
	if err := app.container.Wait(ctx); err != nil && ctx.Err() == nil {
		runErr = fmt.Errorf("app: supervised component: %w", err)
	}
	app.cancel()
	return errors.Join(runErr, app.shutdown())
}

// Shutdown requests cancellation. Run returns after cleanup; this method does not wait.
func (app *Application) Shutdown() {
	app.cancel()
}

func (app *Application) shutdown() error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(app.ctx), app.shutdownTimeout)
	defer cancel()
	if err := app.container.Stop(ctx); err != nil {
		log.Error().Err(err).Msg("shutdown completed with errors")
		return fmt.Errorf("app: shutdown: %w", err)
	}
	return nil
}
