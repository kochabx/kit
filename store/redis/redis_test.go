package redis

import (
	"context"
	"errors"
	"testing"
)

// mockSingleConfig 用于测试单机配置
func mockSingleConfig() *SingleConfig {
	return &SingleConfig{
		Password: "12345678",
	}
}

// mockClusterConfig 用于测试集群配置
func mockClusterConfig() *ClusterConfig {
	return &ClusterConfig{
		Addrs:    []string{":6379", ":6380", ":6381"},
		Password: "12345678",
	}
}

func TestNewSingleClientAndPingClose(t *testing.T) {
	config := mockSingleConfig()
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.Client == nil {
		t.Fatal("Client should not be nil after NewClient")
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if client.Client != nil {
		t.Error("Client should be nil after Close")
	}
}

func TestSingleStats(t *testing.T) {
	config := mockSingleConfig()
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	stats := client.Stats()
	if stats == nil {
		t.Error("Stats should not return nil")
	}

	if client.GetClient() == nil {
		t.Error("GetClient should not return nil")
	}

	_ = client.Close()
}

func TestNewSingleClientInvalidConfig(t *testing.T) {
	_, err := NewClient(nil)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestSinglePingNotInitialized(t *testing.T) {
	client := &Single{}
	if err := client.Ping(context.Background()); !errors.Is(err, ErrClientNotInitialized) {
		t.Errorf("expected ErrClientNotInitialized, got %v", err)
	}
}

func TestSingleStatsNotInitialized(t *testing.T) {
	client := &Single{}
	stats := client.Stats()
	if stats != nil {
		t.Error("Stats should return nil for uninitialized client")
	}
}

func TestSingleCloseNotInitialized(t *testing.T) {
	client := &Single{}
	if err := client.Close(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestNewClusterClientAndPingClose(t *testing.T) {
	config := mockClusterConfig()
	client, err := NewClusterClient(config)
	if err != nil {
		t.Fatalf("NewClusterClient failed: %v", err)
	}
	if client.Client == nil {
		t.Fatal("Client should not be nil after NewClusterClient")
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if client.Client != nil {
		t.Error("Client should be nil after Close")
	}
}

func TestClusterStats(t *testing.T) {
	config := mockClusterConfig()
	client, err := NewClusterClient(config)
	if err != nil {
		t.Fatalf("NewClusterClient failed: %v", err)
	}

	stats := client.Stats()
	if stats == nil {
		t.Error("Stats should not return nil")
	}

	if client.GetClient() == nil {
		t.Error("GetClient should not return nil")
	}

	_ = client.Close()
}

func TestNewClusterClientInvalidConfig(t *testing.T) {
	_, err := NewClusterClient(nil)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestClusterPingNotInitialized(t *testing.T) {
	client := &Cluster{}
	if err := client.Ping(context.Background()); !errors.Is(err, ErrClientNotInitialized) {
		t.Errorf("expected ErrClientNotInitialized, got %v", err)
	}
}

func TestClusterStatsNotInitialized(t *testing.T) {
	client := &Cluster{}
	stats := client.Stats()
	if stats != nil {
		t.Error("Stats should return nil for uninitialized client")
	}
}

func TestClusterCloseNotInitialized(t *testing.T) {
	client := &Cluster{}
	if err := client.Close(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// 测试配置初始化和默认值
func TestSingleConfigDefaults(t *testing.T) {
	config := &SingleConfig{}
	if err := config.Init(); err != nil {
		t.Fatalf("Config init failed: %v", err)
	}

	if config.Host != "localhost" {
		t.Errorf("expected host 'localhost', got %s", config.Host)
	}
	if config.Port != 6379 {
		t.Errorf("expected port 6379, got %d", config.Port)
	}
	if config.DB != 0 {
		t.Errorf("expected DB 0, got %d", config.DB)
	}
	if config.Protocol != 3 {
		t.Errorf("expected protocol 3, got %d", config.Protocol)
	}
}

func TestClusterConfigDefaults(t *testing.T) {
	config := &ClusterConfig{}
	if err := config.Init(); err != nil {
		t.Fatalf("Config init failed: %v", err)
	}

	if len(config.Addrs) != 1 || config.Addrs[0] != "localhost:6379" {
		t.Errorf("expected addrs ['localhost:6379'], got %v", config.Addrs)
	}
	if config.Protocol != 3 {
		t.Errorf("expected protocol 3, got %d", config.Protocol)
	}
}

// 测试通用客户端创建器
func TestNewRedisClient(t *testing.T) {
	// 测试单机模式
	singleConfig := mockSingleConfig()
	client, err := NewRedisClient(singleConfig)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// 测试是否能获取真实的单机客户端
	if redisClient, ok := SingleClient(client); !ok || redisClient == nil {
		t.Error("Should be able to get single client")
	}

	_ = client.Close()

	// 测试集群模式
	clusterConfig := mockClusterConfig()
	clusterClient, err := NewRedisClient(clusterConfig)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}

	if err := clusterClient.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// 测试是否能获取真实的集群客户端
	if redisClusterClient, ok := ClusterClient(clusterClient); !ok || redisClusterClient == nil {
		t.Error("Should be able to get cluster client")
	}

	_ = clusterClient.Close()
}

func TestNewRedisClientInvalidConfig(t *testing.T) {
	_, err := NewRedisClient(nil)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// 测试客户端类型检查
func TestClientTypeCheck(t *testing.T) {
	singleConfig := mockSingleConfig()
	client, err := NewRedisClient(singleConfig)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}

	// 测试单机客户端不能转换为集群客户端
	if _, ok := ClusterClient(client); ok {
		t.Error("Single client should not be convertible to cluster client")
	}

	_ = client.Close()

	clusterConfig := mockClusterConfig()
	clusterClient, err := NewRedisClient(clusterConfig)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}

	// 测试集群客户端不能转换为单机客户端
	if _, ok := SingleClient(clusterClient); ok {
		t.Error("Cluster client should not be convertible to single client")
	}

	_ = clusterClient.Close()
}
