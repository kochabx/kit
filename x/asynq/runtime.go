package asynq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/hibiken/asynq"
	goredis "github.com/redis/go-redis/v9"
)

// Option configures Client, Server, and Scheduler wrappers.
type Option func(*runtimeOptions)

type runtimeOptions struct {
	logger      *slog.Logger
	otelOpts    []OTelOption
	middlewares []asynq.MiddlewareFunc
	configHook  func(*asynq.Config)
	schedHook   func(*asynq.SchedulerOpts)
}

// WithLogger injects a structured logger bridged to asynq.Logger (default discard).
func WithLogger(logger *slog.Logger) Option {
	return func(o *runtimeOptions) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithOTelOptions passes options to the automatic OTelMiddleware on Server.
func WithOTelOptions(opts ...OTelOption) Option {
	return func(o *runtimeOptions) {
		o.otelOpts = append(o.otelOpts, opts...)
	}
}

// WithMiddleware appends additional asynq middlewares executed around handler processing.
func WithMiddleware(mws ...asynq.MiddlewareFunc) Option {
	return func(o *runtimeOptions) {
		for _, mw := range mws {
			if mw != nil {
				o.middlewares = append(o.middlewares, mw)
			}
		}
	}
}

// WithServerConfigHook customizes asynq.Config before creating Server.
func WithServerConfigHook(fn func(*asynq.Config)) Option {
	return func(o *runtimeOptions) {
		if fn != nil {
			o.configHook = fn
		}
	}
}

// WithSchedulerOptsHook customizes asynq.SchedulerOpts before creating Scheduler.
func WithSchedulerOptsHook(fn func(*asynq.SchedulerOpts)) Option {
	return func(o *runtimeOptions) {
		if fn != nil {
			o.schedHook = fn
		}
	}
}

func resolveOptions(opts ...Option) runtimeOptions {
	o := runtimeOptions{logger: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

func resolveRedisClient(cfg Config) (rc goredis.UniversalClient, owned bool) {
	if cfg.RedisClient != nil {
		return cfg.RedisClient, false
	}
	if cfg.RedisOpt != nil {
		if client, ok := cfg.RedisOpt.MakeRedisClient().(goredis.UniversalClient); ok {
			return client, true
		}
	}
	return nil, false
}

func pingRedis(ctx context.Context, pinger goredis.UniversalClient, fallback func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pinger == nil {
		return fallback()
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline || ctx.Done() == nil {
		return pinger.Ping(ctx).Err()
	}
	done := make(chan error, 1)
	go func() {
		done <- pinger.Ping(ctx).Err()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Client is a lifecycle-managed thin wrapper exposing a native *asynq.Client.
type Client struct {
	client    *asynq.Client
	pinger    goredis.UniversalClient
	shared    bool
	closeOnce func() error
}

// NewClient constructs a Client from either Config.RedisClient or Config.RedisOpt.
func NewClient(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	rc, owned := resolveRedisClient(cfg)
	var ac *asynq.Client
	if rc != nil {
		ac = asynq.NewClientFromRedisClient(rc)
	} else {
		ac = asynq.NewClient(cfg.RedisOpt)
	}
	return &Client{
		client: ac,
		pinger: rc,
		shared: !owned,
		closeOnce: sync.OnceValue(func() error {
			if rc != nil {
				if owned {
					return rc.Close()
				}
				return nil
			}
			return ac.Close()
		}),
	}, nil
}

// Client exposes the native *asynq.Client directly without wrapping task APIs.
func (c *Client) Client() *asynq.Client { return c.client }

// Start verifies Redis connectivity for cx lifecycle integration.
func (c *Client) Start(ctx context.Context) error { return c.HealthCheck(ctx) }

// HealthCheck pings the backing Redis connection.
func (c *Client) HealthCheck(ctx context.Context) error {
	return pingRedis(ctx, c.pinger, c.client.Ping)
}

// Stop closes owned resources idempotently.
func (c *Client) Stop(context.Context) error { return c.Close() }

// Close closes owned Redis connections (no-op when sharing an external UniversalClient).
func (c *Client) Close() error { return c.closeOnce() }

// Server is a cx-compatible wrapper exposing a native *asynq.Server.
type Server struct {
	server     *asynq.Server
	handler    asynq.Handler
	pinger     goredis.UniversalClient
	ownedRedis goredis.UniversalClient
	stopOnce   sync.Once
	stopDone   chan struct{}
	stopErr    error
}

// NewServer creates an asynq Server bridged with slog and optional OpenTelemetry middleware.
func NewServer(cfg Config, handler asynq.Handler, opts ...Option) (*Server, error) {
	if handler == nil {
		return nil, errors.New("asynq: handler is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	o := resolveOptions(opts...)
	acfg := cfg.serverConfig(newSlogAdapter(o.logger))
	if o.configHook != nil {
		o.configHook(&acfg)
	}

	wrapped := handler
	for _, mw := range slices.Backward(o.middlewares) {
		wrapped = mw(wrapped)
	}
	if cfg.EnableTracing == nil || *cfg.EnableTracing {
		wrapped = OTelMiddleware(o.otelOpts...)(wrapped)
	}

	rc, owned := resolveRedisClient(cfg)
	var srv *asynq.Server
	if rc != nil {
		srv = asynq.NewServerFromRedisClient(rc, acfg)
	} else {
		srv = asynq.NewServer(cfg.RedisOpt, acfg)
	}
	var ownedRedis goredis.UniversalClient
	if owned {
		ownedRedis = rc
	}
	return &Server{server: srv, handler: wrapped, pinger: rc, ownedRedis: ownedRedis, stopDone: make(chan struct{})}, nil
}

// Server exposes the native *asynq.Server directly.
func (s *Server) Server() *asynq.Server { return s.server }

// Start launches the worker server asynchronously.
func (s *Server) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.server.Start(s.handler); err != nil {
		return fmt.Errorf("asynq: start server: %w", err)
	}
	return nil
}

// Run starts the worker server, blocks until ctx is canceled, and gracefully shuts down.
func (s *Server) Run(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return s.Stop(context.WithoutCancel(ctx))
}

// Stop gracefully shuts down the server while respecting ctx cancellation.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.stopOnce.Do(func() {
		go func() {
			s.server.Shutdown()
			if s.ownedRedis != nil {
				s.stopErr = s.ownedRedis.Close()
			}
			close(s.stopDone)
		}()
	})
	select {
	case <-s.stopDone:
		return s.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HealthCheck pings the backing Redis connection.
func (s *Server) HealthCheck(ctx context.Context) error {
	return pingRedis(ctx, s.pinger, s.server.Ping)
}

// Scheduler is a cx-compatible wrapper exposing a native *asynq.Scheduler.
type Scheduler struct {
	scheduler  *asynq.Scheduler
	pinger     goredis.UniversalClient
	ownedRedis goredis.UniversalClient
	stopOnce   sync.Once
	stopDone   chan struct{}
	stopErr    error
}

// NewScheduler creates a periodic task Scheduler bridged with slog.
func NewScheduler(cfg Config, opts ...Option) (*Scheduler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	o := resolveOptions(opts...)
	sopts := &asynq.SchedulerOpts{
		Logger:   newSlogAdapter(o.logger),
		Location: cfg.Location,
	}
	if o.schedHook != nil {
		o.schedHook(sopts)
	}

	rc, owned := resolveRedisClient(cfg)
	var sched *asynq.Scheduler
	if rc != nil {
		sched = asynq.NewSchedulerFromRedisClient(rc, sopts)
	} else {
		sched = asynq.NewScheduler(cfg.RedisOpt, sopts)
	}
	var ownedRedis goredis.UniversalClient
	if owned {
		ownedRedis = rc
	}
	return &Scheduler{scheduler: sched, pinger: rc, ownedRedis: ownedRedis, stopDone: make(chan struct{})}, nil
}

// Scheduler exposes the native *asynq.Scheduler directly.
func (s *Scheduler) Scheduler() *asynq.Scheduler { return s.scheduler }

// Start launches the scheduler asynchronously.
func (s *Scheduler) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.scheduler.Start(); err != nil {
		return fmt.Errorf("asynq: start scheduler: %w", err)
	}
	return nil
}

// Run starts the scheduler, blocks until ctx is canceled, and shuts down gracefully.
func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return s.Stop(context.WithoutCancel(ctx))
}

// Stop gracefully shuts down the scheduler while respecting ctx cancellation.
func (s *Scheduler) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.stopOnce.Do(func() {
		go func() {
			s.scheduler.Shutdown()
			if s.ownedRedis != nil {
				s.stopErr = s.ownedRedis.Close()
			}
			close(s.stopDone)
		}()
	})
	select {
	case <-s.stopDone:
		return s.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HealthCheck pings the backing Redis connection.
func (s *Scheduler) HealthCheck(ctx context.Context) error {
	return pingRedis(ctx, s.pinger, s.scheduler.Ping)
}
