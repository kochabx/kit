package cx

import (
	"context"
	"fmt"
	"sync"
)

type supervisedComponent struct {
	name   string
	source Supervised
}

type supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	err    error
}

func newSupervisor(parent context.Context, components []supervisedComponent) *supervisor {
	ctx, cancel := context.WithCancel(parent)
	s := &supervisor{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	if len(components) == 0 {
		go func() {
			<-ctx.Done()
			s.complete(ctx.Err())
		}()
		return s
	}
	for _, component := range components {
		go s.watch(component)
	}
	return s
}

func (s *supervisor) watch(component supervisedComponent) {
	select {
	case <-component.source.Done():
		if s.ctx.Err() != nil {
			s.complete(s.ctx.Err())
			return
		}
		err := component.source.Err()
		if err == nil {
			err = ErrComponentExited
		}
		s.complete(fmt.Errorf("component %s: %w", component.name, err))
	case <-s.ctx.Done():
		s.complete(s.ctx.Err())
	}
}

func (s *supervisor) complete(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.done)
		s.cancel()
	})
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

// Wait waits for the shared supervisor result or caller cancellation.
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
