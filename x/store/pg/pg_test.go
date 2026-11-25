package pg_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kochabx/kit/x/store/pg"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     pg.Config
		wantErr bool
	}{
		{
			name: "valid structured",
			cfg: pg.Config{
				Host:     "localhost",
				Database: "app",
				SSLMode:  "require",
			},
		},
		{
			name: "valid dsn",
			cfg: pg.Config{
				DSN: "postgres://user:pass@localhost:5432/app?sslmode=disable",
			},
		},
		{
			name:    "missing host and db",
			cfg:     pg.Config{},
			wantErr: true,
		},
		{
			name: "invalid ssl mode",
			cfg: pg.Config{
				Host:     "localhost",
				Database: "app",
				SSLMode:  "bogus",
			},
			wantErr: true,
		},
		{
			name: "min exceeds max",
			cfg: pg.Config{
				Host:     "localhost",
				Database: "app",
				MaxConns: 2,
				MinConns: 5,
			},
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

func TestSpecialPasswordNoDSNInjectionAndTLSClone(t *testing.T) {
	t.Parallel()

	rawPassword := `p@ss:w/rd?&#%='" dbname=evil`
	callerTLS := &tls.Config{
		ServerName: "pg.internal",
		MinVersion: tls.VersionTLS13,
	}

	cfg := pg.Config{
		Host:          "db.internal",
		Port:          5433,
		Database:      "prod_db",
		Username:      "app:user@role",
		Password:      rawPassword,
		SSLMode:       "verify-full",
		TLS:           callerTLS,
		MaxConns:      1,
		EnableTracing: new(false),
		RuntimeParams: map[string]string{"application_name": "kit-test"},
	}

	poolCfg, err := cfg.BuildPoolConfig(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("BuildPoolConfig failed: %v", err)
	}
	if poolCfg.ConnConfig.Password != rawPassword {
		t.Fatalf("password = %q, want exact %q", poolCfg.ConnConfig.Password, rawPassword)
	}
	if poolCfg.ConnConfig.User != "app:user@role" {
		t.Fatalf("user = %q, want %q", poolCfg.ConnConfig.User, "app:user@role")
	}
	if poolCfg.ConnConfig.Database != "prod_db" {
		t.Fatalf("database = %q, want prod_db", poolCfg.ConnConfig.Database)
	}
	if len(poolCfg.ConnConfig.Fallbacks) != 0 {
		t.Fatalf("expected nil Fallbacks when custom TLS is provided, got %d", len(poolCfg.ConnConfig.Fallbacks))
	}
	if poolCfg.MinConns != 0 {
		t.Fatalf("MinConns = %d when MaxConns=1, want 0", poolCfg.MinConns)
	}

	// Mutating caller TLS must not mutate poolCfg.ConnConfig.TLSConfig.
	callerTLS.ServerName = "mutated.example"
	if poolCfg.ConnConfig.TLSConfig.ServerName != "pg.internal" {
		t.Fatalf("TLSConfig was not cloned, got %q", poolCfg.ConnConfig.TLSConfig.ServerName)
	}
}

func TestLazyNewAndIdempotentClose(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	client, err := pg.New(pg.Config{
		Host:           "127.0.0.1",
		Port:           1,
		Database:       "app",
		ConnectTimeout: 100 * time.Millisecond,
	}, pg.WithLogger(logger))
	if err != nil {
		t.Fatalf("lazy New against unreachable port failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("expected HealthCheck to fail on unreachable port")
	} else if !strings.Contains(err.Error(), "pg: health check") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("Stop/second Close failed: %v", err)
	}
}

func TestDSNPoolPrecedenceAndOverrides(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	// 1. DSN-only pool parameters must not be overwritten by structured defaults.
	dsnOnly := pg.Config{
		DSN: "postgres://user:pass@127.0.0.1:5432/app?sslmode=disable&pool_max_conns=1&pool_min_conns=1&pool_max_conn_lifetime=7m&pool_max_conn_idle_time=3m&pool_health_check_period=15s&connect_timeout=4",
	}
	p1, err := dsnOnly.BuildPoolConfig(logger)
	if err != nil {
		t.Fatalf("dsnOnly BuildPoolConfig failed: %v", err)
	}
	if p1.MaxConns != 1 || p1.MinConns != 1 || p1.MaxConnLifetime != 7*time.Minute ||
		p1.MaxConnIdleTime != 3*time.Minute || p1.HealthCheckPeriod != 15*time.Second ||
		p1.ConnConfig.ConnectTimeout != 4*time.Second {
		t.Fatalf("DSN pool settings clobbered: max=%d min=%d life=%v idle=%v hc=%v conn=%v",
			p1.MaxConns, p1.MinConns, p1.MaxConnLifetime, p1.MaxConnIdleTime, p1.HealthCheckPeriod, p1.ConnConfig.ConnectTimeout)
	}

	// 2. Explicit non-zero struct fields override DSN values.
	override := dsnOnly
	override.MaxConns = 12
	override.MinConns = 4
	override.ConnectTimeout = 9 * time.Second
	p2, err := override.BuildPoolConfig(logger)
	if err != nil {
		t.Fatalf("override BuildPoolConfig failed: %v", err)
	}
	if p2.MaxConns != 12 || p2.MinConns != 4 || p2.ConnConfig.ConnectTimeout != 9*time.Second || p2.MaxConnLifetime != 7*time.Minute {
		t.Fatalf("unexpected explicit override values: max=%d min=%d conn=%v life=%v",
			p2.MaxConns, p2.MinConns, p2.ConnConfig.ConnectTimeout, p2.MaxConnLifetime)
	}

	// 3. Invalid effective min_conns > max_conns fails validation.
	bad := pg.Config{
		DSN:      "postgres://user:pass@127.0.0.1:5432/app?sslmode=disable&pool_min_conns=8",
		MaxConns: 2,
	}
	if _, err := bad.BuildPoolConfig(logger); err == nil {
		t.Fatal("expected error when effective min_conns > max_conns")
	}

	// 4. Structured config still receives full defaults.
	structured := pg.Config{Host: "127.0.0.1", Database: "app"}
	p4, err := structured.BuildPoolConfig(logger)
	if err != nil {
		t.Fatalf("structured BuildPoolConfig failed: %v", err)
	}
	if p4.MaxConns != 25 || p4.MinConns != 2 || p4.ConnConfig.ConnectTimeout != 5*time.Second {
		t.Fatalf("structured defaults mismatch: max=%d min=%d conn=%v", p4.MaxConns, p4.MinConns, p4.ConnConfig.ConnectTimeout)
	}
}
