package cx

import (
	"context"
	"time"
)

// Option configures a Container.
type Option func(*Container)

// WithShutdownTimeout bounds the complete shutdown sequence.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.shutdownTimeout = timeout
		}
	}
}

// WithComponentStopTimeout bounds each individual component Stop call.
func WithComponentStopTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.componentStopTimeout = timeout
		}
	}
}

// WithRollbackTimeout bounds cleanup after startup failure.
func WithRollbackTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.rollbackTimeout = timeout
		}
	}
}

// WithHealthTimeout bounds each component health check.
func WithHealthTimeout(timeout time.Duration) Option {
	return func(c *Container) {
		if timeout > 0 {
			c.healthTimeout = timeout
		}
	}
}

func WithOnStart(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStart })
}

func WithOnStarted(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStarted })
}

func WithOnStopping(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStopping })
}

func WithOnStop(fn func(context.Context) error) Option {
	return appendHook(fn, func(c *Container) *[]func(context.Context) error { return &c.onStop })
}

func appendHook(
	fn func(context.Context) error,
	selectHooks func(*Container) *[]func(context.Context) error,
) Option {
	return func(c *Container) {
		if fn == nil {
			return
		}
		hooks := selectHooks(c)
		*hooks = append(*hooks, fn)
	}
}

// ProvideOption configures a component registration.
type ProvideOption func(*provider) error

// DependsOn declares lifecycle dependencies that are not constructor inputs.
func DependsOn(dependencies ...Dependency) ProvideOption {
	return func(p *provider) error {
		for _, dependency := range dependencies {
			if dependency == nil || dependency.dependencyName() == "" {
				return ErrInvalidDependency
			}
			p.declaredDeps = append(p.declaredDeps, dependency.dependencyName())
		}
		return nil
	}
}
