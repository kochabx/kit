package etcd

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// Option configures an etcd Client.
type Option func(*options)

type options struct {
	logger     *slog.Logger
	configHook func(*clientv3.Config)
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithEtcdConfig customizes *clientv3.Config before client initialization.
func WithEtcdConfig(fn func(*clientv3.Config)) Option {
	return func(o *options) {
		if fn != nil {
			o.configHook = fn
		}
	}
}

// Client manages a lazy etcd v3 client and multi-endpoint health probes.
type Client struct {
	etcd        *clientv3.Client
	endpoints   []string
	logger      *slog.Logger
	dialTimeout int64
	statusProbe func(ctx context.Context, ep string) error
	closeOnce   func() error
}

// New creates a lazy etcd v3 Client without blocking on dials.
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

	dialTimeout := cmp.Or(cfg.DialTimeout, defaultDialTimeout)
	eps := slices.Clone(cfg.Endpoints)
	v3Cfg := clientv3.Config{
		Endpoints:            eps,
		Username:             cfg.Username,
		Password:             cfg.Password,
		DialTimeout:          dialTimeout,
		DialKeepAliveTime:    cfg.DialKeepAliveTime,
		DialKeepAliveTimeout: cfg.DialKeepAliveTimeout,
		AutoSyncInterval:     cfg.AutoSyncInterval,
		RejectOldCluster:     cfg.RejectOldCluster,
		PermitWithoutStream:  cfg.PermitWithoutStream,
		TLS:                  tlsCfg,
		Logger:               zap.NewNop(),
	}
	if o.configHook != nil {
		o.configHook(&v3Cfg)
	}

	cli, err := clientv3.New(v3Cfg)
	if err != nil {
		return nil, fmt.Errorf("etcd: create client: %w", err)
	}

	return &Client{
		etcd:        cli,
		endpoints:   eps,
		logger:      o.logger,
		dialTimeout: int64(dialTimeout),
		statusProbe: func(ctx context.Context, ep string) error {
			resp, err := cli.Status(ctx, ep)
			if err != nil {
				return err
			}
			if resp != nil {
				if len(resp.Errors) > 0 {
					return fmt.Errorf("etcd alarm: %s", strings.Join(resp.Errors, "; "))
				}
				if resp.Leader == 0 {
					return errors.New("etcd: endpoint has no leader")
				}
			}
			return nil
		},
		closeOnce: sync.OnceValue(func() error {
			if err := cli.Close(); err != nil {
				return fmt.Errorf("etcd: close: %w", err)
			}
			return nil
		}),
	}, nil
}

// Client returns the underlying official *clientv3.Client.
func (c *Client) Client() *clientv3.Client {
	return c.etcd
}

// Start verifies connectivity across all configured etcd endpoints.
func (c *Client) Start(ctx context.Context) error {
	return c.HealthCheck(ctx)
}

// HealthCheck probes every configured etcd endpoint and joins any failures.
func (c *Client) HealthCheck(ctx context.Context) error {
	eps := c.endpoints
	if c.etcd != nil {
		if live := c.etcd.Endpoints(); len(live) > 0 {
			eps = live
		}
	}
	errs := make([]error, len(eps))
	var wg sync.WaitGroup
	for i, ep := range eps {
		wg.Go(func() {
			if err := c.statusProbe(ctx, ep); err != nil {
				errs[i] = fmt.Errorf("endpoint %q: %w", ep, err)
			}
		})
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		c.logger.ErrorContext(ctx, "etcd health check failed", slog.Any("err", err))
		return fmt.Errorf("etcd: health check: %w", err)
	}
	return nil
}

// Stop closes the etcd client idempotently.
func (c *Client) Stop(context.Context) error {
	return c.Close()
}

// Close closes the etcd client idempotently.
func (c *Client) Close() error {
	if c.closeOnce == nil {
		return nil
	}
	return c.closeOnce()
}
