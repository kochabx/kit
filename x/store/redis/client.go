package redis

import (
	"cmp"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
)

// Option configures a Redis Client.
type Option func(*options)

type options struct {
	logger   *slog.Logger
	optsHook func(*goredis.UniversalOptions)
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithUniversalOptions customizes the resolved *redis.UniversalOptions before client creation.
func WithUniversalOptions(fn func(*goredis.UniversalOptions)) Option {
	return func(o *options) {
		if fn != nil {
			o.optsHook = fn
		}
	}
}

// Client manages a lazy go-redis UniversalClient (standalone, sentinel, or cluster).
type Client struct {
	rdb       goredis.UniversalClient
	closeOnce func() error
}

// New creates a lazy Redis Client without establishing network connections.
func New(cfg Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	o := options{
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	var tlsCfg *tls.Config
	if cfg.TLS != nil {
		tlsCfg = cfg.TLS.Clone()
	}

	uopts := &goredis.UniversalOptions{
		Addrs:                 slices.Clone(cfg.Addrs),
		MasterName:            cfg.MasterName,
		IsClusterMode:         cfg.Cluster,
		ClientName:            cfg.ClientName,
		Username:              cfg.Username,
		Password:              cfg.Password,
		SentinelUsername:      cfg.SentinelUsername,
		SentinelPassword:      cfg.SentinelPassword,
		DB:                    cfg.DB,
		PoolSize:              cfg.PoolSize,
		MinIdleConns:          cfg.MinIdleConns,
		DialTimeout:           cmp.Or(cfg.DialTimeout, defaultDialTimeout),
		ReadTimeout:           cmp.Or(cfg.ReadTimeout, defaultReadTimeout),
		WriteTimeout:          cmp.Or(cfg.WriteTimeout, defaultWriteTimeout),
		PoolTimeout:           cmp.Or(cfg.PoolTimeout, defaultPoolTimeout),
		MaxRetries:            cfg.MaxRetries,
		ContextTimeoutEnabled: true,
		TLSConfig:             tlsCfg,
	}
	if o.optsHook != nil {
		o.optsHook(uopts)
	}

	rdb := goredis.NewUniversalClient(uopts)

	if cfg.EnableTracing == nil || *cfg.EnableTracing {
		if err := redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false)); err != nil {
			_ = rdb.Close()
			return nil, fmt.Errorf("redis: instrument tracing: %w", err)
		}
	}
	var metricsCloseCh chan struct{}
	if cfg.EnableMetrics == nil || *cfg.EnableMetrics {
		metricsCloseCh = make(chan struct{})
		if err := redisotel.InstrumentMetrics(rdb, redisotel.WithCloseChan(metricsCloseCh)); err != nil {
			close(metricsCloseCh)
			_ = rdb.Close()
			return nil, fmt.Errorf("redis: instrument metrics: %w", err)
		}
	}

	slowThreshold := cmp.Or(cfg.SlowQueryThreshold, defaultSlowQueryThreshold)
	rdb.AddHook(newSlogHook(o.logger, slowThreshold))

	return &Client{
		rdb: rdb,
		closeOnce: sync.OnceValue(func() error {
			if metricsCloseCh != nil {
				close(metricsCloseCh)
			}
			if err := rdb.Close(); err != nil {
				return fmt.Errorf("redis: close: %w", err)
			}
			return nil
		}),
	}, nil
}

// Client returns the underlying redis.UniversalClient.
func (c *Client) Client() goredis.UniversalClient {
	return c.rdb
}

// Start verifies connectivity by pinging Redis.
func (c *Client) Start(ctx context.Context) error {
	return c.HealthCheck(ctx)
}

// HealthCheck pings Redis.
func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: health check: %w", err)
	}
	return nil
}

// Stop closes the Redis client idempotently.
func (c *Client) Stop(context.Context) error {
	return c.Close()
}

// Close closes the Redis client idempotently.
func (c *Client) Close() error {
	if c.closeOnce == nil {
		return nil
	}
	return c.closeOnce()
}
