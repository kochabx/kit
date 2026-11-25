package postgres_test

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kochabx/kit/x/store/gorm/postgres"
)

func TestBuildConnConfigSpecialPasswordAndTLSClone(t *testing.T) {
	t.Parallel()

	rawPassword := `p@ss:w/rd?&#%='" host=evil`
	callerTLS := &tls.Config{ServerName: "pg.internal", MinVersion: tls.VersionTLS13}

	connCfg, err := postgres.BuildConnConfig(postgres.Config{
		Host:          "db.internal",
		Port:          5433,
		Database:      "orders",
		Username:      "svc:user",
		Password:      rawPassword,
		SSLMode:       "verify-full",
		TLS:           callerTLS,
		RuntimeParams: map[string]string{"search_path": "public"},
	})
	if err != nil {
		t.Fatalf("BuildConnConfig failed: %v", err)
	}
	if connCfg.Password != rawPassword {
		t.Fatalf("password = %q, want %q", connCfg.Password, rawPassword)
	}
	if len(connCfg.Fallbacks) != 0 {
		t.Fatalf("expected nil Fallbacks when custom TLS set, got %d", len(connCfg.Fallbacks))
	}

	callerTLS.ServerName = "mutated.example"
	if connCfg.TLSConfig.ServerName != "pg.internal" {
		t.Fatalf("TLSConfig not cloned, got %q", connCfg.TLSConfig.ServerName)
	}

	simpleCfg, err := postgres.BuildConnConfig(postgres.Config{
		Host:                 "db.internal",
		Database:             "orders",
		PreferSimpleProtocol: true,
	})
	if err != nil {
		t.Fatalf("BuildConnConfig PreferSimpleProtocol failed: %v", err)
	}
	if simpleCfg.DefaultQueryExecMode != pgx.QueryExecModeSimpleProtocol {
		t.Fatalf("DefaultQueryExecMode = %v, want SimpleProtocol", simpleCfg.DefaultQueryExecMode)
	}
}

func TestConfigValidateAndLazyNew(t *testing.T) {
	t.Parallel()

	if _, err := postgres.New(postgres.Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}

	client, err := postgres.New(postgres.Config{
		Host:           "127.0.0.1",
		Port:           1,
		Database:       "app",
		ConnectTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("lazy New failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("expected HealthCheck error for unreachable host")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
