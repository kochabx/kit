package cx

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

func (c *Container) Start(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	c.mu.Lock()
	if !c.idleLocked() {
		state := c.state
		c.mu.Unlock()
		return fmt.Errorf("%w: cannot start from %s", ErrInvalidState, state)
	}
	c.resetInstancesLocked()
	c.state = StateStarting
	keys := slices.Clone(c.keys)
	c.mu.Unlock()

	fail := func(startErr error) error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.rollbackTimeout)
		defer cancel()
		rollbackErr := c.stopOwned(rollbackCtx)

		c.mu.Lock()
		if rollbackErr != nil {
			c.state = StateStopFailed
		} else {
			c.resetInstancesLocked()
			c.state = StateFailed
		}
		c.mu.Unlock()
		return errors.Join(startErr, rollbackErr)
	}

	for _, name := range keys {
		if err := ctx.Err(); err != nil {
			return fail(fmt.Errorf("build cancelled: %w", err))
		}
		if err := c.build(name); err != nil {
			return fail(err)
		}
	}
	for _, hook := range c.onStart {
		if err := hook(ctx); err != nil {
			return fail(fmt.Errorf("onStart: %w", err))
		}
	}

	c.mu.RLock()
	order := slices.Clone(c.buildOrder)
	c.mu.RUnlock()
	for _, name := range order {
		if err := ctx.Err(); err != nil {
			return fail(fmt.Errorf("start cancelled: %w", err))
		}
		c.mu.RLock()
		value := c.providers[name].value
		c.mu.RUnlock()
		if starter, ok := value.(Starter); ok {
			if err := starter.Start(ctx); err != nil {
				return fail(fmt.Errorf("start %s: %w", name, err))
			}
		}
	}
	for _, hook := range c.onStarted {
		if err := hook(ctx); err != nil {
			return fail(fmt.Errorf("onStarted: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return fail(fmt.Errorf("start cancelled: %w", err))
	}

	c.mu.Lock()
	c.state = StateRunning
	c.supervisor = newSupervisor(ctx, c.supervisedComponentsLocked())
	c.mu.Unlock()
	return nil
}

func (c *Container) Stop(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	c.mu.Lock()
	if c.state != StateRunning && c.state != StateFailed && c.state != StateStopFailed {
		state := c.state
		c.mu.Unlock()
		return fmt.Errorf("%w: cannot stop from %s", ErrInvalidState, state)
	}
	c.state = StateStopping
	if c.supervisor != nil {
		c.supervisor.cancel()
	}
	shutdownTimeout := c.shutdownTimeout
	c.mu.Unlock()

	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	var errs []error
	if err := runHooks(shutdownCtx, c.onStopping, c.onStoppingDone, "onStopping"); err != nil {
		errs = append(errs, err)
	}
	if err := c.stopOwned(shutdownCtx); err != nil {
		errs = append(errs, err)
	}
	if err := runHooks(shutdownCtx, c.onStop, c.onStopDone, "onStop"); err != nil {
		errs = append(errs, err)
	}

	err := errors.Join(errs...)
	c.mu.Lock()
	if err != nil {
		c.state = StateStopFailed
	} else {
		c.resetInstancesLocked()
		c.supervisor = nil
		c.state = StateStopped
	}
	c.mu.Unlock()
	return err
}

func (c *Container) stopOwned(ctx context.Context) error {
	c.mu.RLock()
	order := slices.Clone(c.buildOrder)
	timeout := c.componentStopTimeout
	c.mu.RUnlock()

	var errs []error
	for _, name := range slices.Backward(order) {
		c.mu.RLock()
		p := c.providers[name]
		value, owned := p.value, p.owned
		c.mu.RUnlock()
		stopper, ok := value.(Stopper)
		if !ok || !owned {
			continue
		}

		stopCtx, cancel := context.WithTimeout(ctx, timeout)
		err := stopper.Stop(stopCtx)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", name, err))
			continue
		}
		c.mu.Lock()
		p.owned = false
		c.mu.Unlock()
	}
	return errors.Join(errs...)
}

func runHooks(
	ctx context.Context,
	hooks []func(context.Context) error,
	completed []bool,
	phase string,
) error {
	var errs []error
	for index, hook := range hooks {
		if completed[index] {
			continue
		}
		if err := hook(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s hook %d: %w", phase, index, err))
			continue
		}
		completed[index] = true
	}
	return errors.Join(errs...)
}

func (c *Container) Restart(ctx context.Context) error {
	if err := c.Stop(ctx); err != nil {
		return err
	}
	return c.Start(ctx)
}
