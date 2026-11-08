package app

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/kochabx/kit/cx"
	"github.com/kochabx/kit/transport"
)

type builder struct {
	ctx             context.Context
	shutdownTimeout time.Duration
	signals         []os.Signal
	container       *cx.Container
	cxOpts          []cx.Option
	servers         []transport.Server
	components      []func(*cx.Container) error
}

// Option configures an application before construction.
type Option func(*builder) error

func WithContext(ctx context.Context) Option {
	return func(b *builder) error {
		if isNil(ctx) {
			return fmt.Errorf("%w: nil context", ErrInvalidOption)
		}
		b.ctx = ctx
		return nil
	}
}

func WithShutdownTimeout(timeout time.Duration) Option {
	return func(b *builder) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: shutdown timeout must be positive", ErrInvalidOption)
		}
		b.shutdownTimeout = timeout
		return nil
	}
}

// WithSignals replaces the default signals. An empty list disables signal handling.
func WithSignals(signals ...os.Signal) Option {
	captured := slices.Clone(signals)
	return func(b *builder) error {
		for _, signal := range captured {
			if isNil(signal) {
				return fmt.Errorf("%w: nil signal", ErrInvalidOption)
			}
		}
		b.signals = slices.Clone(captured)
		return nil
	}
}

// WithContainer transfers lifecycle management to the application. Configure its
// components and hooks beforehand; application registration options cannot be mixed.
func WithContainer(container *cx.Container) Option {
	return func(b *builder) error {
		if container == nil {
			return fmt.Errorf("%w: nil container", ErrInvalidOption)
		}
		b.container = container
		return nil
	}
}

func WithServer(server transport.Server) Option {
	return WithServers(server)
}

func WithServers(servers ...transport.Server) Option {
	captured := slices.Clone(servers)
	return func(b *builder) error {
		for index, server := range captured {
			if isNil(server) {
				return fmt.Errorf("%w: nil server at index %d", ErrInvalidOption, index)
			}
		}
		b.servers = append(b.servers, captured...)
		return nil
	}
}

// WithComponent registers infrastructure before servers, independently of option
// order. Components start in dependency order and stop in reverse order.
func WithComponent[T any](key cx.Key[T], value T) Option {
	return func(b *builder) error {
		if key.String() == "" || isNil(value) {
			return fmt.Errorf("%w: component needs a key and non-nil value", ErrInvalidOption)
		}
		b.components = append(b.components, func(c *cx.Container) error { return c.Supply(key, value) })
		return nil
	}
}

func WithOnStart(fn func(context.Context) error) Option {
	return hookOption(fn, cx.WithOnStart)
}

func WithOnStarted(fn func(context.Context) error) Option {
	return hookOption(fn, cx.WithOnStarted)
}

func WithOnStopping(fn func(context.Context) error) Option {
	return hookOption(fn, cx.WithOnStopping)
}

func WithOnStop(fn func(context.Context) error) Option {
	return hookOption(fn, cx.WithOnStop)
}

func hookOption(fn func(context.Context) error, option func(func(context.Context) error) cx.Option) Option {
	return func(b *builder) error {
		if fn == nil {
			return fmt.Errorf("%w: nil lifecycle hook", ErrInvalidOption)
		}
		b.cxOpts = append(b.cxOpts, option(fn))
		return nil
	}
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
