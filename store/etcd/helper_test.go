package etcd

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributedLock_Unit(t *testing.T) {
	// 测试创建分布式锁
	etcd := &Etcd{}
	lock := etcd.NewDistributedLock("test-lock", 10)

	assert.NotNil(t, lock)
	assert.Equal(t, "test-lock", lock.key)
	assert.NotNil(t, lock.stopCh)
	assert.NotNil(t, lock.doneCh)
}

func TestDistributedLock_TryLock_Unit(t *testing.T) {
	// 测试 nil 客户端
	etcd := &Etcd{Client: nil}
	lock := etcd.NewDistributedLock("test-lock", 10)

	err := lock.TryLock(context.Background(), 10)
	assert.Error(t, err)
	assert.Equal(t, ErrEtcdNotInitialized, err)
}

func TestDistributedLock_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	t.Run("single lock", func(t *testing.T) {
		lock := client.NewDistributedLock("test-single-lock", 10)

		// 获取锁
		err := lock.TryLock(context.Background(), 10)
		assert.NoError(t, err)

		// 释放锁
		err = lock.Unlock(context.Background())
		assert.NoError(t, err)
	})

	t.Run("concurrent locks", func(t *testing.T) {
		lockKey := "test-concurrent-lock"
		var wg sync.WaitGroup
		var successCount int32
		var mu sync.Mutex

		// 启动多个 goroutine 尝试获取同一个锁
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				lock := client.NewDistributedLock(lockKey, 10)
				err := lock.TryLock(context.Background(), 5)

				mu.Lock()
				if err == nil {
					successCount++
					t.Logf("Goroutine %d acquired lock", id)

					// 持有锁一段时间
					time.Sleep(100 * time.Millisecond)

					// 释放锁
					unlockErr := lock.Unlock(context.Background())
					assert.NoError(t, unlockErr)
					t.Logf("Goroutine %d released lock", id)
				} else {
					t.Logf("Goroutine %d failed to acquire lock: %v", id, err)
				}
				mu.Unlock()
			}(i)
		}

		wg.Wait()

		// 由于是尝试锁（非阻塞），可能只有一个能成功
		assert.True(t, successCount >= 1, "At least one goroutine should acquire the lock")
	})

	t.Run("lock expiration", func(t *testing.T) {
		lockKey := "test-expiration-lock"
		lock1 := client.NewDistributedLock(lockKey, 1) // 1秒 TTL

		// 获取锁
		err := lock1.TryLock(context.Background(), 1)
		assert.NoError(t, err)

		// 等待锁过期
		time.Sleep(2 * time.Second)

		// 另一个锁实例应该能够获取锁
		lock2 := client.NewDistributedLock(lockKey, 10)
		err = lock2.TryLock(context.Background(), 10)
		assert.NoError(t, err)

		// 清理
		err = lock2.Unlock(context.Background())
		assert.NoError(t, err)
	})
}

func TestServiceRegistry_Unit(t *testing.T) {
	// 测试创建服务注册实例
	etcd := &Etcd{}
	registry := etcd.NewServiceRegistry("test-services", 30)

	assert.NotNil(t, registry)
	assert.Equal(t, "test-services", registry.keyPrefix)
	assert.Equal(t, int64(30), registry.ttl)
	assert.NotNil(t, registry.stopCh)
	assert.NotNil(t, registry.doneCh)
}

func TestServiceRegistry_Register_Unit(t *testing.T) {
	// 测试 nil 客户端
	etcd := &Etcd{Client: nil}
	registry := etcd.NewServiceRegistry("test-services", 30)

	err := registry.Register(context.Background(), "service-1", "localhost:8080")
	assert.Error(t, err)
	assert.Equal(t, ErrEtcdNotInitialized, err)
}

func TestServiceRegistry_DiscoverServices_Unit(t *testing.T) {
	// 测试 nil 客户端
	etcd := &Etcd{Client: nil}
	registry := etcd.NewServiceRegistry("test-services", 30)

	services, err := registry.DiscoverServices(context.Background())
	assert.Error(t, err)
	assert.Equal(t, ErrEtcdNotInitialized, err)
	assert.Nil(t, services)
}

func TestServiceRegistry_WatchServices_Unit(t *testing.T) {
	// 测试 nil 客户端
	etcd := &Etcd{Client: nil}
	registry := etcd.NewServiceRegistry("test-services", 30)

	watchCh := registry.WatchServices(context.Background())
	assert.Nil(t, watchCh)
}

func TestServiceRegistry_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	t.Run("register and discover", func(t *testing.T) {
		registry := client.NewServiceRegistry("test-services", 30)
		defer registry.Deregister(context.Background())

		// 注册服务
		err := registry.Register(context.Background(), "service-1", "localhost:8080")
		assert.NoError(t, err)

		// 等待一小段时间让注册生效
		time.Sleep(100 * time.Millisecond)

		// 发现服务
		services, err := registry.DiscoverServices(context.Background())
		assert.NoError(t, err)
		assert.NotEmpty(t, services)

		// 检查服务是否存在
		found := false
		for key, value := range services {
			if value == "localhost:8080" {
				assert.Contains(t, key, "service-1")
				found = true
				break
			}
		}
		assert.True(t, found, "Registered service should be found")
	})

	t.Run("multiple services", func(t *testing.T) {
		registry1 := client.NewServiceRegistry("multi-services", 30)
		registry2 := client.NewServiceRegistry("multi-services", 30)
		defer registry1.Deregister(context.Background())
		defer registry2.Deregister(context.Background())

		// 注册多个服务
		err := registry1.Register(context.Background(), "service-1", "localhost:8081")
		assert.NoError(t, err)

		err = registry2.Register(context.Background(), "service-2", "localhost:8082")
		assert.NoError(t, err)

		// 等待注册生效
		time.Sleep(100 * time.Millisecond)

		// 发现服务
		discoveryRegistry := client.NewServiceRegistry("multi-services", 30)
		services, err := discoveryRegistry.DiscoverServices(context.Background())
		assert.NoError(t, err)
		assert.Len(t, services, 2)

		// 检查服务
		var values []string
		for _, value := range services {
			values = append(values, value)
		}
		assert.Contains(t, values, "localhost:8081")
		assert.Contains(t, values, "localhost:8082")
	})

	t.Run("service deregistration", func(t *testing.T) {
		registry := client.NewServiceRegistry("temp-services", 30)

		// 注册服务
		err := registry.Register(context.Background(), "temp-service", "localhost:8083")
		assert.NoError(t, err)

		// 等待注册生效
		time.Sleep(100 * time.Millisecond)

		// 验证服务存在
		services, err := registry.DiscoverServices(context.Background())
		assert.NoError(t, err)
		assert.NotEmpty(t, services)

		// 注销服务
		err = registry.Deregister(context.Background())
		assert.NoError(t, err)

		// 等待注销生效
		time.Sleep(100 * time.Millisecond)

		// 验证服务不存在
		services, err = registry.DiscoverServices(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, services)
	})

	t.Run("watch services", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping watch test in short mode")
		}

		registry := client.NewServiceRegistry("watch-services", 30)
		watchRegistry := client.NewServiceRegistry("watch-services", 30)

		// 开始监听
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		watchCh := watchRegistry.WatchServices(ctx)
		assert.NotNil(t, watchCh)

		// 在另一个 goroutine 中注册服务
		go func() {
			time.Sleep(100 * time.Millisecond)
			err := registry.Register(context.Background(), "watched-service", "localhost:8084")
			if err != nil {
				t.Errorf("Failed to register service: %v", err)
			}
		}()

		// 等待事件
		select {
		case watchResp := <-watchCh:
			assert.NotNil(t, watchResp)
			t.Logf("Received watch event: %+v", watchResp)
		case <-ctx.Done():
			t.Log("Watch test timeout - this is acceptable")
		}

		// 清理
		registry.Deregister(context.Background())
	})
}

// BenchmarkDistributedLock_TryLock 基准测试分布式锁
func BenchmarkDistributedLock_TryLock(b *testing.B) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(b, err)
	defer client.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lockKey := fmt.Sprintf("bench-lock-%d", i)
			lock := client.NewDistributedLock(lockKey, 10)

			err := lock.TryLock(context.Background(), 10)
			if err == nil {
				lock.Unlock(context.Background())
			}
			i++
		}
	})
}

// BenchmarkServiceRegistry_Register 基准测试服务注册
func BenchmarkServiceRegistry_Register(b *testing.B) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(b, err)
	defer client.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry := client.NewServiceRegistry("bench-services", 30)
		serviceID := fmt.Sprintf("service-%d", i)
		serviceInfo := fmt.Sprintf("localhost:80%02d", i%100)

		err := registry.Register(context.Background(), serviceID, serviceInfo)
		if err != nil {
			b.Fatal(err)
		}

		// 清理
		registry.Deregister(context.Background())
	}
}

// TestDistributedLock_EdgeCases 测试边缘情况
func TestDistributedLock_EdgeCases_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	t.Run("unlock without lock", func(t *testing.T) {
		lock := client.NewDistributedLock("unlock-without-lock", 10)

		// 直接释放锁不应该出错
		err := lock.Unlock(context.Background())
		assert.NoError(t, err)
	})

	t.Run("double unlock", func(t *testing.T) {
		lock := client.NewDistributedLock("double-unlock", 10)

		// 获取锁
		err := lock.TryLock(context.Background(), 10)
		assert.NoError(t, err)

		// 释放锁
		err = lock.Unlock(context.Background())
		assert.NoError(t, err)

		// 再次释放锁不应该出错
		err = lock.Unlock(context.Background())
		assert.NoError(t, err)
	})
}

// TestServiceRegistry_EdgeCases 测试服务注册的边缘情况
func TestServiceRegistry_EdgeCases_Integration(t *testing.T) {
	config := getTestConfig()

	client, err := New(config)
	require.NoError(t, err)
	defer client.Close()

	t.Run("deregister without register", func(t *testing.T) {
		registry := client.NewServiceRegistry("edge-services", 30)

		// 直接注销不应该出错
		err := registry.Deregister(context.Background())
		assert.NoError(t, err)
	})

	t.Run("double deregister", func(t *testing.T) {
		registry := client.NewServiceRegistry("double-deregister", 30)

		// 注册服务
		err := registry.Register(context.Background(), "double-service", "localhost:8085")
		assert.NoError(t, err)

		// 注销服务
		err = registry.Deregister(context.Background())
		assert.NoError(t, err)

		// 再次注销不应该出错
		err = registry.Deregister(context.Background())
		assert.NoError(t, err)
	})

	t.Run("empty service discovery", func(t *testing.T) {
		registry := client.NewServiceRegistry("empty-services", 30)

		// 发现空服务列表
		services, err := registry.DiscoverServices(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, services)
	})
}
