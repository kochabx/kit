package gorm

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	gormlib "gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
)

// Option configures a GORM Client.
type Option func(*options)

type options struct {
	logger      *slog.Logger
	gormHook    func(*gormlib.Config)
	tracingOpts []tracing.Option
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithGormConfig customizes the underlying *gorm.Config before opening the database.
func WithGormConfig(fn func(*gormlib.Config)) Option {
	return func(o *options) {
		if fn != nil {
			o.gormHook = fn
		}
	}
}

// WithTracingOptions passes additional options to the OpenTelemetry tracing plugin.
func WithTracingOptions(opts ...tracing.Option) Option {
	return func(o *options) {
		o.tracingOpts = append(o.tracingOpts, opts...)
	}
}

// Client wraps a GORM database instance and its underlying *sql.DB pool.
type Client struct {
	db        *gormlib.DB
	sqlDB     *sql.DB
	closeOnce func() error
}

// New creates a lazy GORM Client for dialector.
func New(dialector gormlib.Dialector, cfg Config, opts ...Option) (*Client, error) {
	if dialector == nil {
		return nil, errors.New("gorm: dialector is required")
	}
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

	slowThreshold := cmp.Or(cfg.SlowThreshold, defaultSlowThreshold)
	gormCfg := &gormlib.Config{
		Logger:                                   newSlogLogger(o.logger, slowThreshold, cfg.LogRecordNotFound),
		SkipDefaultTransaction:                   cfg.SkipDefaultTransaction,
		PrepareStmt:                              cfg.PrepareStmt,
		DisableAutomaticPing:                     true,
		DisableForeignKeyConstraintWhenMigrating: cfg.DisableForeignKeyConstraintWhenMigrating,
	}
	if o.gormHook != nil {
		o.gormHook(gormCfg)
	}

	db, err := gormlib.Open(dialector, gormCfg)
	if err != nil {
		return nil, fmt.Errorf("gorm: open %s: %w", dialector.Name(), err)
	}

	if cfg.EnableTracing == nil || *cfg.EnableTracing {
		tracingOpts := make([]tracing.Option, 0, len(o.tracingOpts)+1)
		tracingOpts = append(tracingOpts, tracing.WithoutQueryVariables())
		tracingOpts = append(tracingOpts, o.tracingOpts...)
		if err := db.Use(tracing.NewPlugin(tracingOpts...)); err != nil {
			if sdb, dbErr := db.DB(); dbErr == nil && sdb != nil {
				_ = sdb.Close()
			}
			return nil, fmt.Errorf("gorm: register otel plugin: %w", err)
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gorm: unwrap sql.DB: %w", err)
	}

	maxOpen := cmp.Or(cfg.Pool.MaxOpenConns, defaultMaxOpenConns)
	var maxIdle int
	switch {
	case cfg.Pool.MaxIdleConns > 0:
		maxIdle = cfg.Pool.MaxIdleConns
	case cfg.Pool.MaxOpenConns == 0:
		maxIdle = defaultMaxIdleConns
	default:
		maxIdle = min(defaultMaxIdleConns, maxOpen)
	}

	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(cmp.Or(cfg.Pool.ConnMaxLifetime, defaultConnMaxLifetime))
	sqlDB.SetConnMaxIdleTime(cmp.Or(cfg.Pool.ConnMaxIdleTime, defaultConnMaxIdleTime))

	return &Client{
		db:    db,
		sqlDB: sqlDB,
		closeOnce: sync.OnceValue(func() error {
			if err := sqlDB.Close(); err != nil {
				return fmt.Errorf("gorm: close: %w", err)
			}
			return nil
		}),
	}, nil
}

// DB returns the underlying *gorm.DB handle.
func (c *Client) DB() *gormlib.DB {
	return c.db
}

// SQLDB returns the underlying *sql.DB connection pool.
func (c *Client) SQLDB() *sql.DB {
	return c.sqlDB
}

// Start verifies connectivity by pinging the database.
func (c *Client) Start(ctx context.Context) error {
	return c.HealthCheck(ctx)
}

// HealthCheck pings the underlying *sql.DB.
func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("gorm: health check: %w", err)
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

// Close closes the underlying *sql.DB idempotently.
func (c *Client) Close() error {
	return c.closeOnce()
}
