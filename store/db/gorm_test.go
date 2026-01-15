package db

import (
	"context"
	"errors"
	"testing"
)

// mockConfig 用于测试
func mockConfig() DriverConfig {
	return &MysqlConfig{
		Password: "12345678",
	}
}

func TestNewGormAndPingClose(t *testing.T) {
	config := mockConfig()
	g, err := NewGorm(config)
	if err != nil {
		t.Fatalf("NewGorm failed: %v", err)
	}
	if g.DB == nil {
		t.Fatal("DB should not be nil after NewGorm")
	}

	if err := g.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	if err := g.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if g.DB != nil {
		t.Error("DB should be nil after Close")
	}
}

func TestStatsAndGetDB(t *testing.T) {
	config := mockConfig()
	g, err := NewGorm(config)
	if err != nil {
		t.Fatalf("NewGorm failed: %v", err)
	}
	stats, err := g.Stats()
	if err != nil {
		t.Errorf("Stats failed: %v", err)
	}
	if stats.MaxOpenConnections == 0 {
		t.Logf("Stats: %+v", stats)
	}
	if g.GetDB() == nil {
		t.Error("GetDB should not return nil")
	}
	_ = g.Close()
}

func TestNewGormInvalidConfig(t *testing.T) {
	_, err := NewGorm(nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestPingNotInitialized(t *testing.T) {
	g := &Gorm{}
	if err := g.Ping(context.Background()); !errors.Is(err, ErrGormNotInitialized) {
		t.Errorf("expected ErrGormNotInitialized, got %v", err)
	}
}

func TestStatsNotInitialized(t *testing.T) {
	g := &Gorm{}
	_, err := g.Stats()
	if !errors.Is(err, ErrGormNotInitialized) {
		t.Errorf("expected ErrGormNotInitialized, got %v", err)
	}
}

func TestCloseNotInitialized(t *testing.T) {
	g := &Gorm{}
	if err := g.Close(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
