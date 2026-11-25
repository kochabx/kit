package etcd

import (
	"context"
	"crypto/tls"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid endpoints",
			cfg:  Config{Endpoints: []string{"127.0.0.1:2379", "127.0.0.1:2380"}},
		},
		{
			name:    "empty endpoints",
			cfg:     Config{},
			wantErr: true,
		},
		{
			name:    "blank endpoint element",
			cfg:     Config{Endpoints: []string{"127.0.0.1:2379", "  "}},
			wantErr: true,
		},
		{
			name:    "negative dial timeout",
			cfg:     Config{Endpoints: []string{"127.0.0.1:2379"}, DialTimeout: -time.Second},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestMultiEndpointHealthCheckAndTLSClone(t *testing.T) {
	t.Parallel()

	callerTLS := &tls.Config{ServerName: "etcd.internal", MinVersion: tls.VersionTLS13}
	var capturedTLS *tls.Config

	client, err := New(
		Config{
			Endpoints:   []string{"ep1:2379", "ep2:2379", "ep3:2379"},
			DialTimeout: 50 * time.Millisecond,
			TLS:         callerTLS,
		},
		WithEtcdConfig(func(c *clientv3.Config) {
			capturedTLS = c.TLS
		}),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	}()

	callerTLS.ServerName = "mutated.example"
	if capturedTLS == nil || capturedTLS.ServerName != "etcd.internal" {
		t.Fatalf("TLS not cloned, got %+v", capturedTLS)
	}

	// Verify HealthCheck probes every configured endpoint, not just endpoints[0].
	var probeCount atomic.Int32
	client.statusProbe = func(_ context.Context, ep string) error {
		probeCount.Add(1)
		if ep == "ep2:2379" {
			return errors.New("ep2 down")
		}
		return nil
	}

	err = client.HealthCheck(t.Context())
	if err == nil || !strings.Contains(err.Error(), "ep2 down") {
		t.Fatalf("expected failure for ep2, got %v", err)
	}
	if got := probeCount.Load(); got != 3 {
		t.Fatalf("probed %d endpoints, want all 3", got)
	}

	// Empty-key ConfigProvider fails cleanly without nil dereference.
	if _, err := NewConfigProvider(client, "").ReadBytes(t.Context()); err == nil {
		t.Fatal("expected error for empty provider key")
	}

	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("idempotent Close failed: %v", err)
	}
}

type fakeEntry struct {
	val   string
	lease clientv3.LeaseID
}

type fakeStore struct {
	mu          sync.Mutex
	nextLease   int64
	leases      map[clientv3.LeaseID]struct{}
	entries     map[string]fakeEntry
	deleteCalls int
	revokeCalls int
	revokeHook  func(context.Context, clientv3.LeaseID) error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		leases:  make(map[clientv3.LeaseID]struct{}),
		entries: make(map[string]fakeEntry),
	}
}

type fakeKV struct {
	clientv3.KV
	store *fakeStore
}

func (k *fakeKV) Txn(context.Context) clientv3.Txn {
	return &fakeTxn{store: k.store}
}

type fakeTxn struct {
	store *fakeStore
	ops   []clientv3.Op
}

func (tx *fakeTxn) If(...clientv3.Cmp) clientv3.Txn  { return tx }
func (tx *fakeTxn) Else(...clientv3.Op) clientv3.Txn { return tx }
func (tx *fakeTxn) Then(ops ...clientv3.Op) clientv3.Txn {
	tx.ops = append(tx.ops, ops...)
	return tx
}

func (tx *fakeTxn) Commit() (*clientv3.TxnResponse, error) {
	tx.store.mu.Lock()
	defer tx.store.mu.Unlock()
	for _, op := range tx.ops {
		key := string(op.KeyBytes())
		switch {
		case op.IsPut():
			leaseID := clientv3.LeaseID(reflect.ValueOf(op).FieldByName("leaseID").Int())
			tx.store.entries[key] = fakeEntry{val: string(op.ValueBytes()), lease: leaseID}
		case op.IsDelete():
			tx.store.deleteCalls++
			delete(tx.store.entries, key)
		}
	}
	return &clientv3.TxnResponse{Succeeded: true}, nil
}

type fakeLease struct {
	clientv3.Lease
	store *fakeStore
}

func (l *fakeLease) Grant(_ context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	l.store.nextLease++
	id := clientv3.LeaseID(l.store.nextLease)
	l.store.leases[id] = struct{}{}
	return &clientv3.LeaseGrantResponse{ID: id, TTL: ttl}, nil
}

func (l *fakeLease) KeepAlive(ctx context.Context, _ clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	ch := make(chan *clientv3.LeaseKeepAliveResponse)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (l *fakeLease) Revoke(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
	l.store.mu.Lock()
	l.store.revokeCalls++
	hook := l.store.revokeHook
	l.store.mu.Unlock()

	if hook != nil {
		if err := hook(ctx, id); err != nil {
			return nil, err
		}
	}

	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if _, ok := l.store.leases[id]; !ok {
		return nil, rpctypes.ErrLeaseNotFound
	}
	delete(l.store.leases, id)
	for k, v := range l.store.entries {
		if v.lease == id {
			delete(l.store.entries, k)
		}
	}
	return &clientv3.LeaseRevokeResponse{}, nil
}

func newRegistryWithFakeStore(t *testing.T, target string) (*Registry, *fakeStore) {
	t.Helper()
	client, err := New(Config{Endpoints: []string{"127.0.0.1:2379"}, DialTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	origKV, origLease := client.etcd.KV, client.etcd.Lease
	store := newFakeStore()
	client.etcd.KV = &fakeKV{KV: origKV, store: store}
	client.etcd.Lease = &fakeLease{Lease: origLease, store: store}
	t.Cleanup(func() {
		client.etcd.KV = origKV
		client.etcd.Lease = origLease
		_ = client.Close()
	})
	reg, err := NewRegistry(client, target)
	if err != nil {
		t.Fatal(err)
	}
	return reg, store
}

func TestRegistryDeregisterDoesNotDeleteNewInstance(t *testing.T) {
	reg, store := newRegistryWithFakeStore(t, "services/orders")

	deregA, err := reg.Register(t.Context(), "node-1", endpoints.Endpoint{Addr: "10.0.0.1:8080"}, 10)
	if err != nil {
		t.Fatalf("register A failed: %v", err)
	}
	deregB, err := reg.Register(t.Context(), "node-1", endpoints.Endpoint{Addr: "10.0.0.2:8080"}, 10)
	if err != nil {
		t.Fatalf("register B failed: %v", err)
	}

	// Delayed deregister from old instance A must only revoke A's lease, preserving B's entry.
	if err := deregA(t.Context()); err != nil {
		t.Fatalf("deregister A failed: %v", err)
	}

	store.mu.Lock()
	entry, ok := store.entries["services/orders/node-1"]
	delCalls := store.deleteCalls
	store.mu.Unlock()
	if delCalls != 0 {
		t.Fatalf("expected 0 DeleteEndpoint calls, got %d", delCalls)
	}
	if !ok || entry.lease != 2 || !strings.Contains(entry.val, "10.0.0.2:8080") {
		t.Fatalf("instance B entry was clobbered: ok=%v entry=%+v", ok, entry)
	}

	if err := deregB(t.Context()); err != nil {
		t.Fatalf("deregister B failed: %v", err)
	}
	store.mu.Lock()
	remaining := len(store.entries)
	store.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected empty store after deregister B, got %d entries", remaining)
	}
}

func TestRegistryDeregisterRetriesAfterTransientError(t *testing.T) {
	reg, store := newRegistryWithFakeStore(t, "services/orders")

	var attempt atomic.Int32
	store.revokeHook = func(_ context.Context, _ clientv3.LeaseID) error {
		if attempt.Add(1) == 1 {
			return context.DeadlineExceeded
		}
		return nil
	}

	dereg, err := reg.Register(t.Context(), "node-1", endpoints.Endpoint{Addr: "10.0.0.1:8080"}, 10)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := dereg(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first deregister = %v, want DeadlineExceeded", err)
	}
	if err := dereg(t.Context()); err != nil {
		t.Fatalf("second deregister retry failed: %v", err)
	}
	if err := dereg(t.Context()); err != nil {
		t.Fatalf("third idempotent deregister failed: %v", err)
	}

	store.mu.Lock()
	calls := store.revokeCalls
	remaining := len(store.entries)
	store.mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected 2 Revoke RPCs (initial failure + successful retry), got %d", calls)
	}
	if remaining != 0 {
		t.Fatalf("expected endpoint removed after retry, got %d entries", remaining)
	}

	// Server reporting ErrLeaseNotFound also marks deregister done cleanly.
	attempt.Store(0)
	store.revokeCalls = 0
	store.revokeHook = func(_ context.Context, _ clientv3.LeaseID) error {
		return rpctypes.ErrGRPCLeaseNotFound
	}
	deregGone, err := reg.Register(t.Context(), "node-2", endpoints.Endpoint{Addr: "10.0.0.3:8080"}, 10)
	if err != nil {
		t.Fatalf("register gone test failed: %v", err)
	}
	if err := deregGone(t.Context()); err != nil {
		t.Fatalf("deregister with ErrLeaseNotFound = %v, want nil", err)
	}
	if err := deregGone(t.Context()); err != nil {
		t.Fatalf("repeat deregister after ErrLeaseNotFound = %v, want nil", err)
	}
	store.mu.Lock()
	goneCalls := store.revokeCalls
	store.mu.Unlock()
	if goneCalls != 1 {
		t.Fatalf("expected 1 Revoke RPC after ErrLeaseNotFound, got %d", goneCalls)
	}
}
