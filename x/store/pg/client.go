package pg

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Option configures a Client.
type Option func(*options)

type options struct {
	logger   *slog.Logger
	poolHook func(*pgxpool.Config)
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithPoolConfigHook allows customizing the resolved *pgxpool.Config before pool creation.
func WithPoolConfigHook(fn func(*pgxpool.Config)) Option {
	return func(o *options) {
		if fn != nil {
			o.poolHook = fn
		}
	}
}

// Client manages a PostgreSQL pgx connection pool.
type Client struct {
	pool      *pgxpool.Pool
	closeOnce func() error
}

// New creates a PostgreSQL pgxpool client without blocking on dials.
// When MinConns > 0, pgxpool warms minimum connections asynchronously in the background
// while Pool() is immediately usable for wiring and sqlc integration.
func New(cfg Config, opts ...Option) (*Client, error) {
	o := options{
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	poolCfg, err := cfg.BuildPoolConfig(o.logger)
	if err != nil {
		return nil, err
	}
	if o.poolHook != nil {
		o.poolHook(poolCfg)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("pg: create pool: %w", err)
	}

	return &Client{
		pool: pool,
		closeOnce: sync.OnceValue(func() error {
			pool.Close()
			return nil
		}),
	}, nil
}

// Pool exposes the underlying *pgxpool.Pool for sqlc or native SQL execution.
func (c *Client) Pool() *pgxpool.Pool {
	return c.pool
}

// Start verifies connectivity to PostgreSQL.
func (c *Client) Start(ctx context.Context) error {
	return c.HealthCheck(ctx)
}

// HealthCheck pings the database using the pool.
func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pg: health check: %w", err)
	}
	return nil
}

// Stop closes the connection pool while respecting ctx cancellation.
func (c *Client) Stop(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- c.Close()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close closes the pool idempotently.
func (c *Client) Close() error {
	return c.closeOnce()
}

type queryStartKey struct{}

type queryStart struct {
	started time.Time
	sql     string
}

type slogQueryTracer struct {
	logger        *slog.Logger
	slowThreshold time.Duration
}

func (t *slogQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{
		started: time.Now(),
		sql:     data.SQL,
	})
}

func (t *slogQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	info, ok := ctx.Value(queryStartKey{}).(queryStart)
	if !ok {
		return
	}
	d := time.Since(info.started)
	switch {
	case data.Err != nil:
		t.logger.ErrorContext(ctx, "pg query failed",
			slog.Duration("duration", d),
			slog.String("sql", info.sql),
			slog.Any("err", data.Err),
		)
	case t.slowThreshold > 0 && d >= t.slowThreshold:
		t.logger.WarnContext(ctx, "slow pg query",
			slog.Duration("duration", d),
			slog.Duration("threshold", t.slowThreshold),
			slog.String("sql", info.sql),
		)
	case t.logger.Enabled(ctx, slog.LevelDebug):
		t.logger.DebugContext(ctx, "pg query ok",
			slog.Duration("duration", d),
			slog.String("sql", info.sql),
		)
	}
}
