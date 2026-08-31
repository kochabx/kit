package mongo

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	tests := []*Config{
		nil,
		{Hosts: []string{""}},
		{Hosts: []string{"one:27017", "two:27017"}, Direct: true},
		{Hosts: []string{"localhost:27017"}, MaxPoolSize: 1, MinPoolSize: 2},
		{Hosts: []string{"localhost:27017"}, ConnectTimeout: -time.Second},
	}
	for _, cfg := range tests {
		client, err := New(cfg)
		if client != nil || !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("New(%#v) = (%v, %v), want ErrInvalidConfig", cfg, client, err)
		}
	}
}

func TestNewDoesNotMutateConfig(t *testing.T) {
	cfg := &Config{Hosts: []string{"localhost:27017"}}
	original := *cfg
	original.Hosts = append([]string(nil), cfg.Hosts...)

	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if !reflect.DeepEqual(original, *cfg) {
		t.Fatalf("New mutated config: got %#v, want %#v", *cfg, original)
	}
}

func TestClientOptions(t *testing.T) {
	cfg, err := resolveConfig(Config{
		Hosts:       []string{"mongo-1:27017", "mongo-2:27017"},
		Username:    "user:@/name",
		Password:    "p@ss:/word",
		AuthSource:  "admin",
		ReplicaSet:  "rs0",
		MaxPoolSize: 50,
		MinPoolSize: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := clientOptions(cfg)
	if !reflect.DeepEqual(opts.Hosts, cfg.Hosts) {
		t.Fatalf("hosts = %v, want %v", opts.Hosts, cfg.Hosts)
	}
	if opts.Auth == nil || opts.Auth.Username != cfg.Username || opts.Auth.Password != cfg.Password || opts.Auth.AuthSource != cfg.AuthSource {
		t.Fatalf("unexpected auth options: %#v", opts.Auth)
	}
	if opts.ReplicaSet == nil || *opts.ReplicaSet != "rs0" {
		t.Fatalf("replica set = %v, want rs0", opts.ReplicaSet)
	}
	if opts.MaxPoolSize == nil || *opts.MaxPoolSize != 50 || opts.MinPoolSize == nil || *opts.MinPoolSize != 5 {
		t.Fatalf("unexpected pool options: max=%v min=%v", opts.MaxPoolSize, opts.MinPoolSize)
	}
}

func TestDatabase(t *testing.T) {
	client, err := New(&Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	database := client.Database("app")
	if database == nil || database.Client() == nil {
		t.Fatal("Database returned an invalid handle")
	}
}

func TestPingIntegration(t *testing.T) {
	if os.Getenv("KIT_MONGO_INTEGRATION") == "" {
		t.Skip("set KIT_MONGO_INTEGRATION=1 to run MongoDB integration tests")
	}
	cfg := &Config{
		Username: "root",
		Password: "12345678",
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
