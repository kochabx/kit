package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Client owns a MongoDB client and its connection pools.
type Client struct {
	client *mongo.Client
}

// New creates a lazy MongoDB client without modifying cfg. It validates driver
// options but does not verify that the deployment is reachable; call Ping or
// Start when a connection check is required.
func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is required", ErrInvalidConfig)
	}
	resolved, err := resolveConfig(*cfg)
	if err != nil {
		return nil, err
	}

	underlying, err := mongo.Connect(clientOptions(resolved))
	if err != nil {
		return nil, fmt.Errorf("mongo: create client: %w", err)
	}
	return &Client{client: underlying}, nil
}

func clientOptions(cfg Config) *options.ClientOptions {
	result := options.Client().
		SetHosts(append([]string(nil), cfg.Hosts...)).
		SetServerAPIOptions(options.ServerAPI(options.ServerAPIVersion1)).
		SetBSONOptions(&options.BSONOptions{
			UseJSONStructTags: true,
			NilSliceAsEmpty:   true,
		}).
		SetMaxPoolSize(cfg.MaxPoolSize).
		SetMinPoolSize(cfg.MinPoolSize).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetServerSelectionTimeout(cfg.ServerSelectionTimeout).
		SetDirect(cfg.Direct)

	if cfg.Username != "" || cfg.Password != "" || cfg.AuthSource != "" {
		result.SetAuth(options.Credential{
			Username:    cfg.Username,
			Password:    cfg.Password,
			PasswordSet: cfg.Password != "",
			AuthSource:  cfg.AuthSource,
		})
	}
	if cfg.ReplicaSet != "" {
		result.SetReplicaSet(cfg.ReplicaSet)
	}
	return result
}

// Database returns a handle for name. Creating a handle does not perform I/O.
func (c *Client) Database(name string) *mongo.Database {
	return c.client.Database(name)
}

// Ping verifies that the primary MongoDB server is reachable using ctx.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("mongo: ping: %w", err)
	}
	return nil
}

// Close disconnects the client and releases its connection pools.
func (c *Client) Close() error {
	return c.disconnect(context.Background())
}

func (c *Client) disconnect(ctx context.Context) error {
	if err := c.client.Disconnect(ctx); err != nil {
		return fmt.Errorf("mongo: disconnect: %w", err)
	}
	return nil
}

// Start verifies the connection when Client is used as an application component.
func (c *Client) Start(ctx context.Context) error { return c.Ping(ctx) }

// Stop disconnects the client using the application shutdown context.
func (c *Client) Stop(ctx context.Context) error { return c.disconnect(ctx) }

// HealthCheck reports whether the primary MongoDB server is reachable.
func (c *Client) HealthCheck(ctx context.Context) error { return c.Ping(ctx) }
