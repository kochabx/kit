package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockMySQLConfig 用于测试的 MySQL 配置
func mockMySQLConfig() DriverConfig {
	return &MySQLConfig{
		Host:     "localhost",
		Port:     3306,
		User:     "root",
		Password: "12345678",
		Database: "test",
	}
}

func TestNew(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer client.Close()

	if client.DB() == nil {
		t.Fatal("DB should not be nil")
	}
}

func TestNewWithOptions(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg,
		WithConnectTimeout(5*time.Second),
		WithSlowQuery(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New with options failed: %v", err)
	}
	defer client.Close()

	if client.DB() == nil {
		t.Fatal("DB should not be nil")
	}
}

func TestIsHealthy(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer client.Close()

	if !client.IsHealthy() {
		t.Error("client should be healthy")
	}
}

func TestNewInvalidConfig(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestPing(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer client.Close()

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestClose(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 再次关闭应该是幂等的
	if err := client.Close(); err != nil {
		t.Errorf("Second Close should succeed, got %v", err)
	}
}

func TestStats(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer client.Close()

	stats := client.Stats()
	t.Logf("Stats: %+v", stats)
}

func TestPingAfterClose(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_ = client.Close()

	if err := client.Ping(context.Background()); err == nil {
		t.Error("expected error after close, got nil")
	}
}

func TestIsHealthyAfterClose(t *testing.T) {
	cfg := mockMySQLConfig()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_ = client.Close()

	if client.IsHealthy() {
		t.Error("client should not be healthy after close")
	}
}

func TestMySQLConfigDSN(t *testing.T) {
	cfg := &MySQLConfig{
		Host:     "localhost",
		Port:     3306,
		User:     "root",
		Password: "password",
		Database: "testdb",
		Charset:  "utf8mb4",
	}

	_ = cfg.Init() // 应用默认值

	dsn := cfg.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}
	t.Logf("MySQL DSN: %s", dsn)
}

func TestPostgresConfigDSN(t *testing.T) {
	cfg := &PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "admin",
		Password: "admin",
		Database: "app",
	}

	_ = cfg.Init()

	dsn := cfg.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}
	t.Logf("Postgres DSN: %s", dsn)
}

func TestSQLiteConfigDSN(t *testing.T) {
	cfg := &SQLiteConfig{
		FilePath: "./test.db",
	}

	_ = cfg.Init()

	dsn := cfg.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}
	t.Logf("SQLite DSN: %s", dsn)
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"silent", LogLevelSilent},
		{"error", LogLevelError},
		{"warn", LogLevelWarn},
		{"info", LogLevelInfo},
		{"SILENT", LogLevelSilent},
		{"unknown", LogLevelSilent},
	}

	for _, tt := range tests {
		if got := ParseLogLevel(tt.input); got != tt.expected {
			t.Errorf("ParseLogLevel(%s) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
