package redis

import (
	"context"
	"testing"
	"time"

	"github.com/kochabx/kit/log"
)

// TestSingleMode 测试单机模式
func TestSingleMode(t *testing.T) {
	ctx := context.Background()

	client, err := New(ctx, Single("localhost:6379"),
		WithPassword("12345678"),
		WithDB(0),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	// 测试 Ping
	if err := client.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// 测试基本操作
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

	// 清理
	client.UniversalClient().Del(ctx, key)
}

// TestClusterMode 测试集群模式
func TestClusterMode(t *testing.T) {
	ctx := context.Background()

	client, err := New(ctx, Cluster("localhost:7000", "localhost:7001", "localhost:7002"),
		WithPassword("12345678"),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis cluster not available): %v", err)
		return
	}
	defer client.Close()

	// 测试 Ping
	if err := client.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// 测试连接池统计
	stats := client.Stats()
	if stats == nil {
		t.Error("Stats should not be nil")
	}
}

// TestSentinelMode 测试哨兵模式
func TestSentinelMode(t *testing.T) {
	ctx := context.Background()

	client, err := New(ctx, Sentinel("mymaster", "localhost:26379", "localhost:26380"),
		WithPassword("12345678"),
		WithDB(0),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis sentinel not available): %v", err)
		return
	}
	defer client.Close()

	// 测试 Ping
	if err := client.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

// TestWithMetrics 测试 Metrics
func TestWithMetrics(t *testing.T) {
	ctx := context.Background()

	client, err := New(ctx, Single("localhost:6379"),
		WithPassword("12345678"),
		WithMetrics(),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	// 执行一些命令
	for i := 0; i < 10; i++ {
		client.UniversalClient().Ping(ctx)
	}

	// OpenTelemetry Metrics 通过 exporter 导出，这里只验证客户端正常工作
	t.Log("Metrics enabled with OpenTelemetry redisotel")
}

// TestWithSlowQuery 测试慢查询检测
func TestWithSlowQuery(t *testing.T) {
	ctx := context.Background()

	logger := log.New()

	client, err := New(ctx, Single("localhost:6379"),
		WithPassword("12345678"),
		WithDebug(1*time.Microsecond), // 设置极小的阈值
		WithLogger(logger),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	// 执行命令（应该触发慢查询日志）
	client.UniversalClient().Ping(ctx)
}

// TestClose 测试关闭客户端
func TestClose(t *testing.T) {
	ctx := context.Background()

	client, err := New(ctx, Single("localhost:6379"),
		WithPassword("12345678"),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}

	// 关闭客户端
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 再次关闭应该不报错
	if err := client.Close(); err != nil {
		t.Errorf("Second close should not error: %v", err)
	}

	// 检查状态
	if !client.IsClosed() {
		t.Error("Client should be closed")
	}

	// 关闭后的操作应该报错
	if err := client.Ping(ctx); err != ErrClientClosed {
		t.Errorf("Expected ErrClientClosed, got %v", err)
	}
}

// TestInvalidConfig 测试无效配置
func TestInvalidConfig(t *testing.T) {
	ctx := context.Background()

	// 空地址
	_, err := New(ctx, &Config{})
	if err != ErrEmptyAddrs {
		t.Errorf("Expected ErrEmptyAddrs, got %v", err)
	}

	// nil 配置
	_, err = New(ctx, nil)
	if err != ErrInvalidConfig {
		t.Errorf("Expected ErrInvalidConfig, got %v", err)
	}
}

// TestConcurrentAccess 测试并发访问
func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()

	client, err := New(ctx, Single("localhost:6379"),
		WithPassword("12345678"),
		WithPoolSize(20),
	)
	if err != nil {
		t.Skipf("Skipping test (Redis not available): %v", err)
		return
	}
	defer client.Close()

	// 并发执行命令
	const goroutines = 50
	const commands = 10

	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < commands; j++ {
				key := "test:concurrent"
				_ = client.UniversalClient().Incr(ctx, key).Err()
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 验证结果
	result, err := client.UniversalClient().Get(ctx, "test:concurrent").Int()
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}

	expected := goroutines * commands
	if result != expected {
		t.Errorf("Expected %d, got %d", expected, result)
	}

	// 清理
	client.UniversalClient().Del(ctx, "test:concurrent")
}

// TestConfigHelpers 测试配置辅助函数
func TestConfigHelpers(t *testing.T) {
	// 测试 Single
	cfg := Single("localhost:6379")
	if !cfg.IsSingle() {
		t.Error("Should be single mode")
	}
	if cfg.IsCluster() || cfg.IsSentinel() {
		t.Error("Should not be cluster or sentinel mode")
	}

	// 测试 Cluster
	cfg = Cluster("h1:6379", "h2:6379")
	if !cfg.IsCluster() {
		t.Error("Should be cluster mode")
	}
	if cfg.IsSingle() || cfg.IsSentinel() {
		t.Error("Should not be single or sentinel mode")
	}

	// 测试 Sentinel
	cfg = Sentinel("mymaster", "s1:26379", "s2:26379")
	if !cfg.IsSentinel() {
		t.Error("Should be sentinel mode")
	}
	if cfg.IsSingle() || cfg.IsCluster() {
		t.Error("Should not be single or cluster mode")
	}
}
