package db

import (
	"errors"
	"testing"
)

func TestNewInvalidConfig(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
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
