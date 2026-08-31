package etcd

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Lock is a non-blocking distributed lock backed by an etcd lease.
type Lock struct {
	client *clientv3.Client
	key    string
	ttl    int64

	mu      sync.Mutex
	leaseID clientv3.LeaseID
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewLock creates a lock definition. It does not contact etcd.
func (c *Client) NewLock(key string, ttl time.Duration) (*Lock, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: lock key is required", ErrInvalidConfig)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: lock TTL must be positive", ErrInvalidConfig)
	}
	return &Lock{client: c.client, key: key, ttl: int64(math.Ceil(ttl.Seconds()))}, nil
}

// TryLock acquires the lock immediately or returns ErrLockHeld.
func (l *Lock) TryLock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.leaseID != 0 {
		return ErrLockHeld
	}

	lease, err := l.client.Grant(ctx, l.ttl)
	if err != nil {
		return fmt.Errorf("etcd: grant lock lease: %w", err)
	}
	response, err := l.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(l.key), "=", 0)).
		Then(clientv3.OpPut(l.key, "locked", clientv3.WithLease(lease.ID))).
		Commit()
	if err != nil {
		l.revoke(ctx, lease.ID)
		return fmt.Errorf("etcd: acquire lock: %w", err)
	}
	if !response.Succeeded {
		l.revoke(ctx, lease.ID)
		return ErrLockHeld
	}

	keepAliveCtx, cancel := context.WithCancel(context.Background())
	keepAlive, err := l.client.KeepAlive(keepAliveCtx, lease.ID)
	if err != nil {
		cancel()
		l.revoke(ctx, lease.ID)
		return fmt.Errorf("etcd: keep lock alive: %w", err)
	}
	l.leaseID = lease.ID
	l.cancel = cancel
	l.done = make(chan struct{})
	go drainKeepAlive(l.done, keepAlive)
	return nil
}

// Unlock releases the lock. Calling Unlock on an unlocked Lock is harmless.
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	leaseID, cancel, done := l.leaseID, l.cancel, l.done
	l.leaseID, l.cancel, l.done = 0, nil, nil
	l.mu.Unlock()
	if leaseID == 0 {
		return nil
	}
	cancel()
	<-done
	if _, err := l.client.Revoke(ctx, leaseID); err != nil {
		return fmt.Errorf("etcd: release lock: %w", err)
	}
	return nil
}

func (l *Lock) revoke(ctx context.Context, leaseID clientv3.LeaseID) {
	_, _ = l.client.Revoke(ctx, leaseID)
}

func drainKeepAlive(done chan<- struct{}, keepAlive <-chan *clientv3.LeaseKeepAliveResponse) {
	defer close(done)
	for range keepAlive {
	}
}
