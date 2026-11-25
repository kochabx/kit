package mongo_test

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/kochabx/kit/x/store/mongo"
)

func TestBuildClientOptionsAuthAndTLS(t *testing.T) {
	t.Parallel()

	// Authless mode must NOT set default credentials.
	authlessOpts, err := (mongo.Config{
		Hosts: []string{"127.0.0.1:27017"},
	}).BuildClientOptions()
	if err != nil {
		t.Fatalf("BuildClientOptions authless failed: %v", err)
	}
	if authlessOpts.Auth != nil {
		t.Fatalf("expected nil Auth when Username is empty, got %+v", authlessOpts.Auth)
	}

	// Authenticated mode defaults AuthSource to "admin" when omitted.
	callerTLS := &tls.Config{ServerName: "mongo.internal", MinVersion: tls.VersionTLS13}
	authOpts, err := (mongo.Config{
		Hosts:    []string{"127.0.0.1:27017"},
		Username: "app",
		Password: "secret",
		TLS:      callerTLS,
	}).BuildClientOptions()
	if err != nil {
		t.Fatalf("BuildClientOptions auth failed: %v", err)
	}
	if authOpts.Auth == nil || authOpts.Auth.AuthSource != "admin" {
		t.Fatalf("expected default AuthSource=admin, got %+v", authOpts.Auth)
	}

	callerTLS.ServerName = "mutated.example"
	if authOpts.TLSConfig == nil || authOpts.TLSConfig.ServerName != "mongo.internal" {
		t.Fatalf("TLSConfig not cloned, got %+v", authOpts.TLSConfig)
	}

	// Custom AuthSource overrides default.
	customOpts, err := (mongo.Config{
		URI:        "mongodb://127.0.0.1:27017",
		Username:   "app",
		Password:   "secret",
		AuthSource: "app_db",
	}).BuildClientOptions()
	if err != nil {
		t.Fatalf("BuildClientOptions custom AuthSource failed: %v", err)
	}
	if customOpts.Auth == nil || customOpts.Auth.AuthSource != "app_db" {
		t.Fatalf("expected AuthSource=app_db, got %+v", customOpts.Auth)
	}

	if _, err := (mongo.Config{URI: "invalid-uri"}).BuildClientOptions(); err == nil {
		t.Fatal("expected BuildClientOptions error for invalid URI")
	}
}

func TestConfigValidateLazyNewAndClose(t *testing.T) {
	t.Parallel()

	if _, err := mongo.New(mongo.Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}

	client, err := mongo.New(mongo.Config{
		Hosts:                  []string{"127.0.0.1:1"},
		Database:               "app",
		ConnectTimeout:         50 * time.Millisecond,
		ServerSelectionTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("lazy New failed: %v", err)
	}
	if db := client.Database(); db == nil || db.Name() != "app" {
		t.Fatalf("expected default database app, got %v", db)
	}
	if db := client.Database("other"); db == nil || db.Name() != "other" {
		t.Fatalf("expected override database other, got %v", db)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("expected HealthCheck to fail on unreachable server")
	}
	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("idempotent Close failed: %v", err)
	}
}
