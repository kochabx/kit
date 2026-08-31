package etcd

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Registry stores service records below a key prefix using leases.
type Registry struct {
	client *clientv3.Client
	prefix string
	ttl    int64

	mu      sync.Mutex
	leaseID clientv3.LeaseID
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewRegistry creates a service registry definition. It does not contact etcd.
func (c *Client) NewRegistry(prefix string, ttl time.Duration) (*Registry, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return nil, fmt.Errorf("%w: registry prefix is required", ErrInvalidConfig)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: registry TTL must be positive", ErrInvalidConfig)
	}
	return &Registry{client: c.client, prefix: prefix + "/", ttl: int64(math.Ceil(ttl.Seconds()))}, nil
}

// Register publishes one service record and keeps its lease alive until Deregister.
func (r *Registry) Register(ctx context.Context, serviceID, value string) error {
	if serviceID == "" || strings.Contains(serviceID, "/") {
		return fmt.Errorf("%w: service ID must be a non-empty path segment", ErrInvalidConfig)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leaseID != 0 {
		return ErrServiceExists
	}

	lease, err := r.client.Grant(ctx, r.ttl)
	if err != nil {
		return fmt.Errorf("etcd: grant service lease: %w", err)
	}
	if _, err := r.client.Put(ctx, r.prefix+serviceID, value, clientv3.WithLease(lease.ID)); err != nil {
		_, _ = r.client.Revoke(ctx, lease.ID)
		return fmt.Errorf("etcd: register service: %w", err)
	}
	keepAliveCtx, cancel := context.WithCancel(context.Background())
	keepAlive, err := r.client.KeepAlive(keepAliveCtx, lease.ID)
	if err != nil {
		cancel()
		_, _ = r.client.Revoke(ctx, lease.ID)
		return fmt.Errorf("etcd: keep service alive: %w", err)
	}
	r.leaseID = lease.ID
	r.cancel = cancel
	r.done = make(chan struct{})
	go drainKeepAlive(r.done, keepAlive)
	return nil
}

// Deregister removes the active service record. Repeated calls are harmless.
func (r *Registry) Deregister(ctx context.Context) error {
	r.mu.Lock()
	leaseID, cancel, done := r.leaseID, r.cancel, r.done
	r.leaseID, r.cancel, r.done = 0, nil, nil
	r.mu.Unlock()
	if leaseID == 0 {
		return nil
	}
	cancel()
	<-done
	if _, err := r.client.Revoke(ctx, leaseID); err != nil {
		return fmt.Errorf("etcd: deregister service: %w", err)
	}
	return nil
}

// Services returns service IDs mapped to their registered values.
func (r *Registry) Services(ctx context.Context) (map[string]string, error) {
	response, err := r.client.Get(ctx, r.prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcd: discover services: %w", err)
	}
	services := make(map[string]string, len(response.Kvs))
	for _, item := range response.Kvs {
		services[strings.TrimPrefix(string(item.Key), r.prefix)] = string(item.Value)
	}
	return services, nil
}

// Watch returns changes to service records below the registry prefix.
func (r *Registry) Watch(ctx context.Context) clientv3.WatchChan {
	return r.client.Watch(ctx, r.prefix, clientv3.WithPrefix())
}
