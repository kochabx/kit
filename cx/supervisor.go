package cx

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

type supervisedComponent struct {
	name   string
	source Supervised
}

type supervisor struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func newSupervisor(parent context.Context, components []supervisedComponent) *supervisor {
	ctx, cancel := context.WithCancel(parent)
	s := &supervisor{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	g, gCtx := errgroup.WithContext(ctx)
	if len(components) == 0 {
		g.Go(func() error {
			<-gCtx.Done()
			return nil
		})
	} else {
		for _, comp := range components {
			g.Go(func() error {
				select {
				case <-comp.source.Done():
					if ctx.Err() != nil {
						return nil
					}
					err := comp.source.Err()
					if err == nil {
						err = ErrComponentExited
					}
					return fmt.Errorf("component %s: %w", comp.name, err)
				case <-gCtx.Done():
					return nil
				}
			})
		}
	}

	go func() {
		defer cancel()
		s.err = g.Wait()
		close(s.done)
	}()
	return s
}

func (c *Container) supervisedComponentsLocked() []supervisedComponent {
	components := make([]supervisedComponent, 0, len(c.buildOrder))
	for _, name := range c.buildOrder {
		if source, ok := c.providers[name].value.(Supervised); ok {
			components = append(components, supervisedComponent{name: name, source: source})
		}
	}
	return components
}

// Wait blocks until a supervised component exits, parent context cancels, or Stop runs.
func (c *Container) Wait(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	c.mu.RLock()
	if c.state != StateRunning {
		state := c.state
		c.mu.RUnlock()
		return fmt.Errorf("%w: cannot wait in %s", ErrInvalidState, state)
	}
	s := c.supervisor
	c.mu.RUnlock()

	select {
	case <-s.done:
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
