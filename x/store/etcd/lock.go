package etcd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Locker acquires lease-backed distributed mutexes using etcd's official concurrency package.
type Locker struct {
	client     *Client
	prefix     string
	ttlSeconds int
}

// NewLocker creates a Locker rooted at prefix with a lease TTL in seconds (default 30s).
func NewLocker(client *Client, prefix string, ttlSeconds int) (*Locker, error) {
	if client == nil || client.etcd == nil {
		return nil, errors.New("etcd: client is required")
	}
	if strings.TrimSpace(prefix) == "" {
		return nil, errors.New("etcd: lock prefix is required")
	}
	if ttlSeconds < 0 {
		return nil, errors.New("etcd: ttlSeconds cannot be negative")
	}
	return &Locker{
		client:     client,
		prefix:     prefix,
		ttlSeconds: cmp.Or(ttlSeconds, defaultLockTTLSeconds),
	}, nil
}

// Acquire opens a ctx-bounded leased concurrency session and acquires the mutex,
// revoking the lease if Lock fails.
func (l *Locker) Acquire(ctx context.Context) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	leaseResp, err := l.client.etcd.Grant(ctx, int64(l.ttlSeconds))
	if err != nil {
		return nil, fmt.Errorf("etcd: grant lock lease: %w", err)
	}

	session, err := concurrency.NewSession(
		l.client.etcd,
		concurrency.WithLease(leaseResp.ID),
		concurrency.WithTTL(l.ttlSeconds),
	)
	if err != nil {
		l.revokeBounded(ctx, leaseResp.ID)
		return nil, fmt.Errorf("etcd: create lock session: %w", err)
	}

	mutex := concurrency.NewMutex(session, l.prefix)
	if err := mutex.Lock(ctx); err != nil {
		l.revokeBounded(ctx, session.Lease())
		session.Orphan()
		return nil, fmt.Errorf("etcd: acquire lock %q: %w", l.prefix, err)
	}

	return &Lock{
		locker:  l,
		session: session,
		mutex:   mutex,
	}, nil
}

func (l *Locker) revokeBounded(ctx context.Context, id clientv3.LeaseID) {
	revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultOperationTimeout)
	defer cancel()
	_, _ = l.client.etcd.Revoke(revokeCtx, id)
}

// Lock represents a held etcd distributed mutex. Callers should monitor Done() for lease loss.
type Lock struct {
	mu       sync.Mutex
	unlocked bool
	locker   *Locker
	session  *concurrency.Session
	mutex    *concurrency.Mutex
}

// Done returns a channel closed if the backing lease expires or loses keepalive.
func (l *Lock) Done() <-chan struct{} {
	return l.session.Done()
}

// Unlock idempotently releases the mutex and revokes its lease session with a bounded timeout.
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.unlocked {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	unlockCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline || ctx.Err() != nil {
		var cancel context.CancelFunc
		unlockCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), defaultOperationTimeout)
		defer cancel()
	}

	unlockErr := l.mutex.Unlock(unlockCtx)
	revokeCtx, cancelRevoke := context.WithTimeout(context.WithoutCancel(ctx), defaultOperationTimeout)
	_, revokeErr := l.locker.client.etcd.Revoke(revokeCtx, l.session.Lease())
	cancelRevoke()
	l.session.Orphan()

	err := errors.Join(unlockErr, revokeErr)
	if unlockErr == nil {
		l.unlocked = true
	}
	return err
}
