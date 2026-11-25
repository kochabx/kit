package etcd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
)

// Registry manages lease-backed service endpoints via etcd's official naming/endpoints package.
type Registry struct {
	client  *Client
	target  string
	manager endpoints.Manager
}

// NewRegistry creates an endpoint registry for target (e.g. "services/orders").
func NewRegistry(client *Client, target string) (*Registry, error) {
	if client == nil || client.etcd == nil {
		return nil, errors.New("etcd: client is required")
	}
	target = strings.TrimSuffix(strings.TrimSpace(target), "/")
	mgr, err := endpoints.NewManager(client.etcd, target)
	if err != nil {
		return nil, fmt.Errorf("etcd: create endpoint manager: %w", err)
	}
	return &Registry{
		client:  client,
		target:  target,
		manager: mgr,
	}, nil
}

// Manager returns the underlying official endpoints.Manager.
func (r *Registry) Manager() endpoints.Manager {
	return r.manager
}

// Register adds ep under key backed by an auto-renewed lease and returns an idempotent deregister func.
func (r *Registry) Register(ctx context.Context, key string, ep endpoints.Endpoint, ttlSeconds int64) (func(context.Context) error, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("etcd: endpoint key is required")
	}
	if ttlSeconds < 0 {
		return nil, errors.New("etcd: ttlSeconds cannot be negative")
	}
	ttl := cmp.Or(ttlSeconds, int64(defaultLockTTLSeconds))

	leaseResp, err := r.client.etcd.Grant(ctx, ttl)
	if err != nil {
		return nil, fmt.Errorf("etcd: grant registration lease: %w", err)
	}

	keepCtx, cancelKeep := context.WithCancel(r.client.etcd.Ctx())
	keepCh, err := r.client.etcd.KeepAlive(keepCtx, leaseResp.ID)
	if err != nil {
		cancelKeep()
		revokeCtx, cancelRevoke := context.WithTimeout(context.WithoutCancel(ctx), defaultOperationTimeout)
		_, _ = r.client.etcd.Revoke(revokeCtx, leaseResp.ID)
		cancelRevoke()
		return nil, fmt.Errorf("etcd: start keepalive: %w", err)
	}

	var drainWG sync.WaitGroup
	drainWG.Go(func() {
		for range keepCh {
		}
	})

	fullKey := r.normalizeKey(key)
	if err := r.manager.AddEndpoint(ctx, fullKey, ep, clientv3.WithLease(leaseResp.ID)); err != nil {
		cancelKeep()
		drainWG.Wait()
		revokeCtx, cancelRevoke := context.WithTimeout(context.WithoutCancel(ctx), defaultOperationTimeout)
		_, _ = r.client.etcd.Revoke(revokeCtx, leaseResp.ID)
		cancelRevoke()
		return nil, fmt.Errorf("etcd: register endpoint %q: %w", fullKey, err)
	}

	var (
		stopKeepOnce sync.Once
		mu           sync.Mutex
		done         bool
	)
	deregister := func(deregCtx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return nil
		}

		stopKeepOnce.Do(func() {
			cancelKeep()
			drainWG.Wait()
		})

		opCtx := deregCtx
		if opCtx == nil || opCtx.Err() != nil {
			var cancel context.CancelFunc
			opCtx, cancel = context.WithTimeout(context.WithoutCancel(cmp.Or(deregCtx, context.Background())), defaultOperationTimeout)
			defer cancel()
		}
		if _, err := r.client.etcd.Revoke(opCtx, leaseResp.ID); err != nil {
			if !errors.Is(err, rpctypes.ErrLeaseNotFound) && !errors.Is(rpctypes.Error(err), rpctypes.ErrLeaseNotFound) {
				return err
			}
		}
		done = true
		return nil
	}
	return deregister, nil
}

func (r *Registry) normalizeKey(key string) string {
	prefix := r.target + "/"
	if strings.HasPrefix(key, prefix) {
		return key
	}
	return prefix + strings.TrimPrefix(key, "/")
}
