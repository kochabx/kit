package etcd

import (
	"context"
	"fmt"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Client owns an etcd client and its connections.
type Client struct {
	client    *clientv3.Client
	endpoints []string
	closeOnce sync.Once
	closeErr  error
}

// New creates a lazy etcd client without modifying cfg. Call Ping or Start
// when the cluster must be verified before use.
func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is required", ErrInvalidConfig)
	}
	resolved, err := resolveConfig(*cfg)
	if err != nil {
		return nil, err
	}
	underlying, err := clientv3.New(clientv3.Config{
		Endpoints:            resolved.Endpoints,
		Username:             resolved.Username,
		Password:             resolved.Password,
		DialTimeout:          resolved.DialTimeout,
		DialKeepAliveTime:    resolved.KeepAliveTime,
		DialKeepAliveTimeout: resolved.KeepAliveTimeout,
		AutoSyncInterval:     resolved.AutoSyncInterval,
		MaxCallSendMsgSize:   resolved.MaxSendMsgSize,
		MaxCallRecvMsgSize:   resolved.MaxRecvMsgSize,
		RejectOldCluster:     resolved.RejectOldCluster,
		PermitWithoutStream:  resolved.PermitWithoutStream,
		TLS:                  resolved.TLS,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd: create client: %w", err)
	}
	return &Client{client: underlying, endpoints: resolved.Endpoints}, nil
}

// Client returns the underlying official etcd client.
func (c *Client) Client() *clientv3.Client { return c.client }

// Ping verifies that one configured endpoint is reachable using ctx.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.client.Status(ctx, c.endpoints[0]); err != nil {
		return fmt.Errorf("etcd: ping: %w", err)
	}
	return nil
}

// Status returns the maintenance status for endpoint.
func (c *Client) Status(ctx context.Context, endpoint string) (*clientv3.StatusResponse, error) {
	response, err := c.client.Status(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("etcd: status: %w", err)
	}
	return response, nil
}

// Close releases the client's resources. It is safe to call more than once.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if err := c.client.Close(); err != nil {
			c.closeErr = fmt.Errorf("etcd: close: %w", err)
		}
	})
	return c.closeErr
}

func (c *Client) Start(ctx context.Context) error       { return c.Ping(ctx) }
func (c *Client) Stop(context.Context) error            { return c.Close() }
func (c *Client) HealthCheck(ctx context.Context) error { return c.Ping(ctx) }
