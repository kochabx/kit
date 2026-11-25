package mongo

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const defaultCloseTimeout = 5 * time.Second

// Option configures a Mongo Client.
type Option func(*optionsBag)

type optionsBag struct {
	logger   *slog.Logger
	optsHook func(*options.ClientOptions)
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *optionsBag) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithClientOptions customizes *options.ClientOptions before creating the MongoDB client.
func WithClientOptions(fn func(*options.ClientOptions)) Option {
	return func(o *optionsBag) {
		if fn != nil {
			o.optsHook = fn
		}
	}
}

// Client manages a lazy MongoDB driver client.
type Client struct {
	client    *mongodriver.Client
	defaultDB string
	logger    *slog.Logger
	closeMu   sync.Mutex
	closed    bool
}

// New creates a lazy MongoDB Client without blocking on network I/O.
func New(cfg Config, opts ...Option) (*Client, error) {
	o := optionsBag{
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	clientOpts, err := cfg.BuildClientOptions()
	if err != nil {
		return nil, err
	}
	if o.optsHook != nil {
		o.optsHook(clientOpts)
	}

	mc, err := mongodriver.Connect(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo: connect: %w", err)
	}

	return &Client{
		client:    mc,
		defaultDB: cfg.Database,
		logger:    o.logger,
	}, nil
}

// Client returns the underlying *mongo.Client.
func (c *Client) Client() *mongodriver.Client {
	return c.client
}

// Database returns a named database handle, falling back to Config.Database when name is omitted.
func (c *Client) Database(name ...string) *mongodriver.Database {
	var dbName string
	if len(name) > 0 {
		dbName = name[0]
	}
	dbName = cmp.Or(dbName, c.defaultDB)
	return c.client.Database(dbName)
}

// Start verifies connectivity against the primary MongoDB node.
func (c *Client) Start(ctx context.Context) error {
	return c.HealthCheck(ctx)
}

// HealthCheck pings the primary MongoDB node.
func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.client.Ping(ctx, readpref.Primary()); err != nil {
		c.logger.ErrorContext(ctx, "mongo health check failed", slog.Any("err", err))
		return fmt.Errorf("mongo: health check: %w", err)
	}
	return nil
}

// Stop disconnects the MongoDB client using ctx deadline and can be retried after failure.
func (c *Client) Stop(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultCloseTimeout)
		defer cancel()
	}
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	if err := c.client.Disconnect(ctx); err != nil {
		return fmt.Errorf("mongo: disconnect: %w", err)
	}
	c.closed = true
	return nil
}

// Close disconnects the MongoDB client idempotently on success.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	return c.Stop(ctx)
}
