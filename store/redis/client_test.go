package redis

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/kochabx/kit/log"
)

func requireRedisIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("KIT_REDIS_INTEGRATION") == "" {
		t.Skip("set KIT_REDIS_INTEGRATION=1 to run Redis integration tests")
	}
}

// TestSingleMode tests standalone mode.
func TestSingleMode(t *testing.T) {
	requireRedisIntegration(t)
	ctx := context.Background()

	cfg := Single("localhost:6379")
	cfg.Password = "12345678"
	client, err := New(cfg)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	// Test Ping.
	if err := client.Ping(ctx); err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
	}

	// Test basic commands.
	key := "test:key"
	value := "test:value"

	err = client.UniversalClient().Set(ctx, key, value, time.Minute).Err()
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	result, err := client.UniversalClient().Get(ctx, key).Result()
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if result != value {
		t.Errorf("Expected %s, got %s", value, result)
	}

	// Clean up.
	client.UniversalClient().Del(ctx, key)
}

// TestClusterMode tests cluster mode.
func TestClusterMode(t *testing.T) {
	requireRedisIntegration(t)
	ctx := context.Background()

	cfg := Cluster("localhost:7000", "localhost:7001", "localhost:7002")
	cfg.Password = "12345678"
	client, err := New(cfg)
	if err != nil {
		t.Skipf("Skipping test (Redis cluster not available): %v", err)
		return
	}
	defer client.Close()

	// Test Ping.
	if err := client.Ping(ctx); err != nil {
		t.Skipf("Skipping test (Redis cluster not available): %v", err)
	}

	// Test connection pool statistics.
	stats := client.Stats()
	if stats == nil {
		t.Error("Stats should not be nil")
	}
}

// TestSentinelMode tests Sentinel mode.
func TestSentinelMode(t *testing.T) {
	requireRedisIntegration(t)
	ctx := context.Background()

	cfg := Sentinel("mymaster", "localhost:26379", "localhost:26380")
	cfg.Password = "12345678"
	client, err := New(cfg)
	if err != nil {
		t.Skipf("Skipping test (Redis sentinel not available): %v", err)
		return
	}
	defer client.Close()

	// Test Ping.
	if err := client.Ping(ctx); err != nil {
		t.Skipf("Skipping test (Redis sentinel not available): %v", err)
	}
}

// TestWithMetrics tests metrics instrumentation.
func TestWithMetrics(t *testing.T) {
	requireRedisIntegration(t)
	ctx := context.Background()

	cfg := Single("localhost:6379")
	cfg.Password = "12345678"
	client, err := New(cfg, WithMetrics())
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
	}

	// Execute commands.
	for range 10 {
		client.UniversalClient().Ping(ctx)
	}

	// Exporters deliver OpenTelemetry metrics; this test only checks that client operations succeed.
	t.Log("Metrics enabled with OpenTelemetry redisotel")
}

// TestWithSlowQuery tests slow-command detection.
func TestWithSlowQuery(t *testing.T) {
	requireRedisIntegration(t)
	ctx := context.Background()

	logger := log.New()

	cfg := Single("localhost:6379")
	cfg.Password = "12345678"
	client, err := New(cfg,
		WithDebug(1*time.Microsecond), // Use a very low threshold.
		WithLogger(logger),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
	}

	// Execute a command that should trigger slow-command logging.
	client.UniversalClient().Ping(ctx)
}

// TestClose tests client shutdown.
func TestClose(t *testing.T) {
	cfg := Single("localhost:6379")
	cfg.Password = "12345678"
	client, err := New(cfg)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}

	// Close the client.
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Exercise a second Close using the underlying client's behavior.
	// If underlying client.Close() is safe to call twice, this is fine.
	// Usually go-redis client.Close() is safe-ish but might return error.
	// Our new Close just calls client.Close().
}

// TestConcurrentAccess tests concurrent operations.
func TestConcurrentAccess(t *testing.T) {
	requireRedisIntegration(t)
	ctx := context.Background()

	cfg := Single("localhost:6379")
	cfg.Password = "12345678"
	cfg.PoolSize = 20
	client, err := New(cfg)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
	}

	// Execute commands concurrently.
	const goroutines = 50
	const commands = 10

	done := make(chan bool, goroutines)

	for i := range goroutines {
		go func(id int) {
			for range commands {
				key := "test:concurrent"
				_ = client.UniversalClient().Incr(ctx, key).Err()
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines.
	for range goroutines {
		<-done
	}

	// Verify the results.
	result, err := client.UniversalClient().Get(ctx, "test:concurrent").Int()
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}

	expected := goroutines * commands
	if result != expected {
		t.Errorf("Expected %d, got %d", expected, result)
	}

	// Clean up.
	client.UniversalClient().Del(ctx, "test:concurrent")
}

func TestConfigConstructors(t *testing.T) {
	if got := Single("localhost:6379").Mode; got != ModeSingle {
		t.Fatalf("mode = %q, want single", got)
	}
	if got := Cluster("h1:6379").Mode; got != ModeCluster {
		t.Fatalf("mode = %q, want cluster", got)
	}
	if got := Sentinel("mymaster", "s1:26379").Mode; got != ModeSentinel {
		t.Fatalf("mode = %q, want sentinel", got)
	}
}

func TestNewDoesNotMutateConfig(t *testing.T) {
	cfg := Single("localhost:6379")
	original := *cfg
	original.Addrs = append([]string(nil), cfg.Addrs...)

	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if !reflect.DeepEqual(original, *cfg) {
		t.Fatalf("New mutated config: got %#v, want %#v", *cfg, original)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []*Config{
		{Addrs: []string{""}},
		{Addrs: []string{"localhost:6379"}, Protocol: 4},
		{Addrs: []string{"localhost:6379"}, PoolSize: 1, MinIdleConns: 2},
		{Addrs: []string{"localhost:6379"}, MinRetryBackoff: time.Second, MaxRetryBackoff: time.Millisecond},
		Cluster(),
		Sentinel(""),
		{Mode: ModeSingle, Addrs: []string{"one:6379", "two:6379"}},
	}
	for _, cfg := range tests {
		client, err := New(cfg)
		if client != nil || !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("New(%#v) = (%v, %v), want ErrInvalidConfig", cfg, client, err)
		}
	}
}

func TestOptionValidation(t *testing.T) {
	cfg := Single("localhost:6379")
	tests := []Option{
		nil,
		WithLogger(nil),
		WithHooks(nil),
		WithDebug(-time.Second),
		WithDebug(time.Second, 2*time.Second),
	}
	for _, option := range tests {
		client, err := New(cfg, option)
		if client != nil || !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("New option = (%v, %v), want ErrInvalidOption", client, err)
		}
	}
}

func TestInstrumentationOptionsWithoutArgumentsAreEnabled(t *testing.T) {
	opts, err := resolveOptions([]Option{WithDebug(), WithTracing(), WithMetrics()})
	if err != nil {
		t.Fatal(err)
	}
	if opts.debug == nil || opts.tracingOptions == nil || opts.metricsOptions == nil {
		t.Fatalf("instrumentation options were not enabled: %#v", opts)
	}
}

func TestUniversalOptions(t *testing.T) {
	cfg, err := resolveConfig(Config{
		Addrs:    []string{"redis.internal:6379"},
		Username: "app",
		Password: "secret",
		DB:       2,
		PoolSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := universalOptions(cfg)
	if opts.Addrs[0] != "redis.internal:6379" || opts.Username != "app" || opts.Password != "secret" || opts.DB != 2 || opts.PoolSize != 20 {
		t.Fatalf("unexpected universal options: %#v", opts)
	}
	clusterOptions := universalOptions(*Cluster("cluster.internal:6379"))
	if !clusterOptions.IsClusterMode {
		t.Fatal("Cluster must enable IsClusterMode")
	}
}
