package mongo

import (
	"context"
	"testing"
	"time"
)

func TestClient_Ping(t *testing.T) {
	m, err := New(&Config{
		Password: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Ping(ctx); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
}

func TestClient_GetClient(t *testing.T) {
	m, err := New(&Config{
		Password: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	client := m.GetClient()
	if client == nil {
		t.Error("GetClient() returned nil")
	}
}

func TestClient_Database(t *testing.T) {
	m, err := New(&Config{
		Password: "12345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	db := m.Database("testdb")
	if db == nil {
		t.Error("Database() returned nil")
	}
}
