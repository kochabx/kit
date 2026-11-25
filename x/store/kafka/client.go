package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Option configures a Kafka Client.
type Option func(*options)

type options struct {
	logger  *slog.Logger
	kgoOpts []kgo.Opt
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithKgoOpts appends low-level franz-go options after resolving Config.
func WithKgoOpts(opts ...kgo.Opt) Option {
	return func(o *options) {
		o.kgoOpts = append(o.kgoOpts, opts...)
	}
}

// Client manages a lazy franz-go Kafka client.
type Client struct {
	kgo       *kgo.Client
	stopped   atomic.Bool
	closeOnce func() error
}

// New creates a lazy Kafka Client without blocking on broker dials.
func New(cfg Config, opts ...Option) (*Client, error) {
	o := options{
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	kopts, err := cfg.BuildKgoOpts(o.logger)
	if err != nil {
		return nil, err
	}
	kopts = append(kopts, o.kgoOpts...)

	cl, err := kgo.NewClient(kopts...)
	if err != nil {
		return nil, fmt.Errorf("kafka: create client: %w", err)
	}

	return &Client{
		kgo: cl,
		closeOnce: sync.OnceValue(func() error {
			cl.Close()
			return nil
		}),
	}, nil
}

// Client returns the underlying *kgo.Client.
func (c *Client) Client() *kgo.Client {
	return c.kgo
}

// Start verifies connectivity against the Kafka cluster.
func (c *Client) Start(ctx context.Context) error {
	return c.HealthCheck(ctx)
}

// HealthCheck pings at least one configured Kafka broker.
func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.kgo.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: health check: %w", err)
	}
	return nil
}

// Stop flushes buffered producer records up to ctx deadline and closes the client idempotently.
func (c *Client) Stop(ctx context.Context) error {
	if !c.stopped.CompareAndSwap(false, true) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	flushErr := c.kgo.Flush(ctx)
	leaveErr := c.kgo.LeaveGroupContext(ctx)
	return errors.Join(flushErr, leaveErr, c.Close())
}

// Close closes the Kafka client idempotently.
func (c *Client) Close() error {
	if c.closeOnce == nil {
		return nil
	}
	return c.closeOnce()
}
