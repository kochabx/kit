package etcd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"
)

// ConfigProvider reads raw configuration payloads stored at an etcd key.
type ConfigProvider struct {
	client *Client
	key    string
}

// NewConfigProvider returns an etcd key reader for remote configuration lookups.
func NewConfigProvider(client *Client, key string) *ConfigProvider {
	return &ConfigProvider{
		client: client,
		key:    key,
	}
}

// Key returns the configured etcd key.
func (p *ConfigProvider) Key() string {
	return p.key
}

// ReadBytes retrieves the raw configuration bytes stored at key in etcd.
func (p *ConfigProvider) ReadBytes(ctx context.Context) ([]byte, error) {
	if p.client == nil || p.client.etcd == nil {
		return nil, errors.New("etcd: client is required")
	}
	if p.key == "" {
		return nil, errors.New("etcd: config key is required")
	}

	timeout := defaultOperationTimeout
	if p.client.dialTimeout > 0 {
		timeout = time.Duration(p.client.dialTimeout)
	}
	opCtx, cancel := context.WithTimeout(cmp.Or(ctx, context.Background()), timeout)
	defer cancel()

	resp, err := p.client.etcd.Get(opCtx, p.key)
	if err != nil {
		return nil, fmt.Errorf("etcd: read config key %q: %w", p.key, err)
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("etcd: config key %q not found", p.key)
	}
	return resp.Kvs[0].Value, nil
}
