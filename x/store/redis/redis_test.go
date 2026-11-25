package redis

import (
	"context"
	"crypto/tls"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kochabx/kit/config"
	goredis "github.com/redis/go-redis/v9"
)

type recordCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *recordCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *recordCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *recordCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *recordCapture) WithGroup(string) slog.Handler      { return c }

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "standalone",
			cfg:  Config{Addrs: []string{"127.0.0.1:6379"}},
		},
		{
			name: "sentinel",
			cfg:  Config{Addrs: []string{"127.0.0.1:26379"}, MasterName: "mymaster"},
		},
		{
			name: "cluster",
			cfg:  Config{Addrs: []string{"127.0.0.1:6379"}, Cluster: true, MaxRetries: -1},
		},
		{
			name:    "cluster with non-zero db",
			cfg:     Config{Addrs: []string{"127.0.0.1:6379"}, Cluster: true, DB: 1},
			wantErr: true,
		},
		{
			name:    "empty addrs",
			cfg:     Config{},
			wantErr: true,
		},
		{
			name:    "cluster and master_name conflict",
			cfg:     Config{Addrs: []string{"127.0.0.1:6379"}, Cluster: true, MasterName: "mymaster"},
			wantErr: true,
		},
		{
			name:    "min exceeds pool",
			cfg:     Config{Addrs: []string{"127.0.0.1:6379"}, PoolSize: 2, MinIdleConns: 5},
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

func TestSlogHookNilAndSlowErrorPreservation(t *testing.T) {
	t.Parallel()

	capHandler := &recordCapture{}
	logger := slog.New(capHandler)
	hook := newSlogHook(logger, 10*time.Millisecond)

	now := time.Unix(100, 0)
	hook.now = func() time.Time { return now }

	cmd := goredis.NewStringCmd(t.Context(), "get", "missing")

	// 1. redis.Nil must NOT emit WARN or ERROR even if slow.
	process := hook.ProcessHook(func(context.Context, goredis.Cmder) error {
		now = now.Add(50 * time.Millisecond)
		return goredis.Nil
	})
	if err := process(t.Context(), cmd); err != goredis.Nil {
		t.Fatalf("expected redis.Nil, got %v", err)
	}
	if len(capHandler.records) != 1 || capHandler.records[0].Level != slog.LevelDebug {
		t.Fatalf("expected single DEBUG record for redis.Nil, got %+v", capHandler.records)
	}

	// 2. Real error that is also slow must log ERROR with the error preserved, not swallowed as WARN.
	capHandler.records = nil
	processErr := hook.ProcessHook(func(context.Context, goredis.Cmder) error {
		now = now.Add(50 * time.Millisecond)
		return context.DeadlineExceeded
	})
	if err := processErr(t.Context(), cmd); err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if len(capHandler.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(capHandler.records))
	}
	rec := capHandler.records[0]
	if rec.Level != slog.LevelError || rec.Message != "redis command failed" {
		t.Fatalf("expected ERROR record with 'redis command failed', got level=%s msg=%q", rec.Level, rec.Message)
	}

	// 3. Slow command without error logs WARN.
	capHandler.records = nil
	processSlow := hook.ProcessHook(func(context.Context, goredis.Cmder) error {
		now = now.Add(50 * time.Millisecond)
		return nil
	})
	if err := processSlow(t.Context(), cmd); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(capHandler.records) != 1 || capHandler.records[0].Level != slog.LevelWarn {
		t.Fatalf("expected WARN record for slow command, got %+v", capHandler.records)
	}
}

func TestNewTLSCloneAndIdempotentClose(t *testing.T) {
	t.Parallel()

	callerTLS := &tls.Config{ServerName: "redis.internal", MinVersion: tls.VersionTLS13}
	var capturedTLS *tls.Config

	client, err := New(
		Config{
			Addrs: []string{"127.0.0.1:1"},
			TLS:   callerTLS,
		},
		WithUniversalOptions(func(u *goredis.UniversalOptions) {
			capturedTLS = u.TLSConfig
		}),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	callerTLS.ServerName = "mutated.example"
	if capturedTLS == nil || capturedTLS.ServerName != "redis.internal" {
		t.Fatalf("TLS not cloned, got %+v", capturedTLS)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("Stop/second Close failed: %v", err)
	}
}

func TestConfigJSONTagDecoding(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "redis.yaml")
	yaml := "addrs:\n  - 127.0.0.1:6379\nmaster_name: primary\npool_size: 16\nmin_idle_conns: 4\nslow_query_threshold: 250ms\nmax_retries: -1\nenable_tracing: false\n"
	if err := os.WriteFile(file, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	loader, err := config.New[Config](config.WithFile(file))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(t.Context())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.MasterName != "primary" || loaded.PoolSize != 16 || loaded.MinIdleConns != 4 || loaded.SlowQueryThreshold != 250*time.Millisecond || loaded.MaxRetries != -1 || loaded.EnableTracing == nil || *loaded.EnableTracing {
		t.Fatalf("json tag fields dropped or misdecoded: %+v", loaded)
	}
}
