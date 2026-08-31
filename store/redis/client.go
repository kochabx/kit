package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/kochabx/kit/log"
)

// Client owns the underlying go-redis client and its connection pools.
type Client struct {
	client redis.UniversalClient
}

// New creates a lazy Redis client without modifying cfg. It does not establish
// a connection; call Ping or Start when the connection must be verified.
func New(cfg *Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is required", ErrInvalidConfig)
	}
	resolved, err := resolveConfig(*cfg)
	if err != nil {
		return nil, err
	}
	settings, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}

	underlying := redis.NewUniversalClient(universalOptions(resolved))
	if err := installHooks(underlying, settings); err != nil {
		_ = underlying.Close()
		return nil, err
	}
	if settings.logger != nil {
		settings.logger.Debug().Str("mode", string(resolved.Mode)).Strs("addrs", resolved.Addrs).Msg("redis client created")
	}
	return &Client{client: underlying}, nil
}

func universalOptions(cfg Config) *redis.UniversalOptions {
	return &redis.UniversalOptions{
		Addrs:      append([]string(nil), cfg.Addrs...),
		MasterName: cfg.MasterName,
		Username:   cfg.Username,
		Password:   cfg.Password,
		DB:         cfg.DB,
		Protocol:   cfg.Protocol,

		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,

		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: cfg.MaxIdleTime,
		ConnMaxLifetime: cfg.MaxLifetime,
		PoolTimeout:     cfg.PoolTimeout,

		MaxRetries:      cfg.MaxRetries,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,

		TLSConfig:      cfg.TLSConfig,
		IsClusterMode:  cfg.Mode == ModeCluster,
		MaxRedirects:   cfg.MaxRedirects,
		ReadOnly:       cfg.ReadOnly,
		RouteByLatency: cfg.RouteByLatency,
		RouteRandomly:  cfg.RouteRandomly,
	}
}

func installHooks(client redis.UniversalClient, opts options) error {
	for _, hook := range opts.hooks {
		client.AddHook(hook)
	}
	if opts.tracingOptions != nil {
		if err := redisotel.InstrumentTracing(client, opts.tracingOptions...); err != nil {
			return fmt.Errorf("redis: install tracing: %w", err)
		}
	}
	if opts.metricsOptions != nil {
		if err := redisotel.InstrumentMetrics(client, opts.metricsOptions...); err != nil {
			return fmt.Errorf("redis: install metrics: %w", err)
		}
	}
	if opts.debug != nil {
		logger := opts.logger
		if logger == nil {
			logger = log.Global()
		}
		client.AddHook(newDebugHook(logger, opts.debug.slowQueryThreshold))
	}
	return nil
}

// UniversalClient returns the underlying go-redis client for Redis commands,
// pipelines, transactions, and scripts.
func (c *Client) UniversalClient() redis.UniversalClient { return c.client }

// Ping verifies that Redis is reachable using ctx.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	return nil
}

// Close releases all connections owned by the client.
func (c *Client) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("redis: close: %w", err)
	}
	return nil
}

// Stats returns a snapshot of connection pool statistics.
func (c *Client) Stats() *redis.PoolStats { return c.client.PoolStats() }

// Start verifies the connection when Client is used as an application component.
func (c *Client) Start(ctx context.Context) error { return c.Ping(ctx) }

// Stop closes the client when it is used as an application component.
func (c *Client) Stop(context.Context) error { return c.Close() }

// HealthCheck reports whether Redis is reachable.
func (c *Client) HealthCheck(ctx context.Context) error { return c.Ping(ctx) }
