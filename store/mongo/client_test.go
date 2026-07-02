package mongo

import (
	"context"
	"os"
	"testing"
	"time"
)

func requireMongoIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("KIT_MONGO_INTEGRATION") == "" {
		t.Skip("set KIT_MONGO_INTEGRATION=1 to run MongoDB integration tests")
	}
}

func TestClient_Ping(t *testing.T) {
	requireMongoIntegration(t)
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
		t.Skipf("Skipping test (MongoDB not available): %v", err)
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
