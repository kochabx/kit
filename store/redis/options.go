package redis

import (
	"fmt"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

type options struct {
	hooks  []redis.Hook
	logger *log.Logger
	debug  *debugOptions

	tracingOptions []redisotel.TracingOption
	metricsOptions []redisotel.MetricsOption
}

type debugOptions struct {
	slowQueryThreshold time.Duration
}

// Option configures runtime dependencies and instrumentation.
type Option func(*options) error

// WithHooks installs custom go-redis hooks.
func WithHooks(hooks ...redis.Hook) Option {
	return func(opts *options) error {
		for index, hook := range hooks {
			if hook == nil {
				return fmt.Errorf("%w: nil hook at index %d", ErrInvalidOption, index)
			}
		}
		opts.hooks = append(opts.hooks, hooks...)
		return nil
	}
}

// WithMetrics enables the official go-redis OpenTelemetry metrics instrumentation.
func WithMetrics(metricsOptions ...redisotel.MetricsOption) Option {
	return func(opts *options) error {
		if opts.metricsOptions == nil {
			opts.metricsOptions = make([]redisotel.MetricsOption, 0, len(metricsOptions))
		}
		opts.metricsOptions = append(opts.metricsOptions, metricsOptions...)
		return nil
	}
}

// WithTracing enables the official go-redis OpenTelemetry tracing instrumentation.
func WithTracing(tracingOptions ...redisotel.TracingOption) Option {
	return func(opts *options) error {
		if opts.tracingOptions == nil {
			opts.tracingOptions = make([]redisotel.TracingOption, 0, len(tracingOptions))
		}
		opts.tracingOptions = append(opts.tracingOptions, tracingOptions...)
		return nil
	}
}

// WithDebug logs command names and execution results without command arguments.
// An optional threshold enables slow-command warnings. Without WithLogger,
// debug output is sent to log.Global().
func WithDebug(slowQueryThreshold ...time.Duration) Option {
	return func(opts *options) error {
		if len(slowQueryThreshold) > 1 {
			return fmt.Errorf("%w: WithDebug accepts at most one slow query threshold", ErrInvalidOption)
		}
		if len(slowQueryThreshold) == 1 {
			if slowQueryThreshold[0] < 0 {
				return fmt.Errorf("%w: slow query threshold must not be negative", ErrInvalidOption)
			}
			opts.debug = &debugOptions{slowQueryThreshold: slowQueryThreshold[0]}
		} else {
			opts.debug = &debugOptions{}
		}
		return nil
	}
}

// WithLogger sets the destination for lifecycle and debug logs. It does not
// enable per-command logging; use WithDebug for that.
func WithLogger(logger *log.Logger) Option {
	return func(opts *options) error {
		if logger == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidOption)
		}
		opts.logger = logger
		return nil
	}
}

func resolveOptions(optionList []Option) (options, error) {
	var opts options
	for index, option := range optionList {
		if option == nil {
			return options{}, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := option(&opts); err != nil {
			return options{}, err
		}
	}
	return opts, nil
}
