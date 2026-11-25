package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/kochabx/kit/cx"
)

type builder struct {
	shutdownTimeout time.Duration
	signals         []os.Signal
	logger          *slog.Logger
	container       *cx.Container
	cxOpts          []cx.Option
	servers         []Server
	runnables       []cx.Runnable
	components      []func(*cx.Container) error
}

// Option configures an Application before construction.
type Option func(*builder) error

func WithShutdownTimeout(timeout time.Duration) Option {
	return func(b *builder) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: shutdown timeout must be positive", ErrInvalidOption)
		}
		b.shutdownTimeout = timeout
		return nil
	}
}

// WithSignals replaces the default termination signals. Passing no signals disables signal handling.
func WithSignals(signals ...os.Signal) Option {
	captured := slices.Clone(signals)
	return func(b *builder) error {
		for _, sig := range captured {
			if isNilValue(sig) {
				return fmt.Errorf("%w: nil signal", ErrInvalidOption)
			}
		}
		b.signals = slices.Clone(captured)
		return nil
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(b *builder) error {
		if logger == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidOption)
		}
		b.logger = logger
		return nil
	}
}

func WithContainer(container *cx.Container) Option {
	return func(b *builder) error {
		if container == nil {
			return fmt.Errorf("%w: nil container", ErrInvalidOption)
		}
		b.container = container
		return nil
	}
}

func WithServers(servers ...Server) Option {
	captured := slices.Clone(servers)
	return func(b *builder) error {
		for index, server := range captured {
			if isNilValue(server) {
				return fmt.Errorf("%w: nil server at index %d", ErrInvalidOption, index)
			}
		}
		b.servers = append(b.servers, captured...)
		return nil
	}
}

func WithRunnable(runnables ...cx.Runnable) Option {
	captured := slices.Clone(runnables)
	return func(b *builder) error {
		for index, r := range captured {
			if isNilValue(r) {
				return fmt.Errorf("%w: nil runnable at index %d", ErrInvalidOption, index)
			}
		}
		b.runnables = append(b.runnables, captured...)
		return nil
	}
}

// WithComponent registers infrastructure before servers independently of option order.
func WithComponent[T any](key cx.Key[T], value T) Option {
	return func(b *builder) error {
		if key.String() == "" || isNilValue(value) {
			return fmt.Errorf("%w: component needs a key and non-nil value", ErrInvalidOption)
		}
		b.components = append(b.components, func(c *cx.Container) error { return c.Supply(key, value) })
		return nil
	}
}

func WithOnStart(fn func(context.Context) error) Option { return hookOption(fn, cx.WithOnStart) }
func WithOnStarted(fn func(context.Context) error) Option {
	return hookOption(fn, cx.WithOnStarted)
}
func WithOnStopping(fn func(context.Context) error) Option {
	return hookOption(fn, cx.WithOnStopping)
}
func WithOnStop(fn func(context.Context) error) Option { return hookOption(fn, cx.WithOnStop) }

func hookOption(fn func(context.Context) error, option func(func(context.Context) error) cx.Option) Option {
	return func(b *builder) error {
		if fn == nil {
			return fmt.Errorf("%w: nil lifecycle hook", ErrInvalidOption)
		}
		b.cxOpts = append(b.cxOpts, option(fn))
		return nil
	}
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
