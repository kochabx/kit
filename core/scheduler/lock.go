package scheduler

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	//go:embed lua/acquire_lock.lua
	acquireLockScript string

	//go:embed lua/release_lock.lua
	releaseLockScript string

	//go:embed lua/extend_lock.lua
	extendLockScript string
)

// DistLock 分布式锁
type DistLock struct {
	client    *redis.Client
	namespace string
}

// NewDistLock 创建分布式锁
func NewDistLock(client *redis.Client, namespace string) *DistLock {
	return &DistLock{
		client:    client,
		namespace: namespace,
	}
}

// buildLockKey 构建锁的完整键名
func (l *DistLock) buildLockKey(key string) string {
	return fmt.Sprintf("%s:lock:%s", l.namespace, key)
}

// Acquire 获取锁
// 返回: (是否成功, error)
func (l *DistLock) Acquire(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	lockKey := l.buildLockKey(key)

	// 使用Lua脚本确保原子性
	result, err := l.client.Eval(ctx, acquireLockScript, []string{lockKey}, value, int(ttl.Seconds())).Result()
	if err != nil {
		return false, err
	}

	// Lua返回1表示成功，0表示失败
	if result == int64(1) {
		return true, nil
	}
	return false, nil
}

// Release 释放锁
// 只有持有锁的客户端才能释放（通过value验证）
func (l *DistLock) Release(ctx context.Context, key string, value string) (bool, error) {
	lockKey := l.buildLockKey(key)

	// 使用Lua脚本确保原子性
	result, err := l.client.Eval(ctx, releaseLockScript, []string{lockKey}, value).Result()
	if err != nil {
		return false, err
	}

	// Lua返回1表示成功，0表示失败（锁不存在或value不匹配）
	return result == int64(1), nil
}

// Extend 延长锁的TTL
func (l *DistLock) Extend(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	lockKey := l.buildLockKey(key)

	// 使用Lua脚本确保原子性
	result, err := l.client.Eval(ctx, extendLockScript, []string{lockKey}, value, int(ttl.Seconds())).Result()
	if err != nil {
		return false, err
	}

	return result == int64(1), nil
}

// IsLocked 检查锁是否存在
func (l *DistLock) IsLocked(ctx context.Context, key string) (bool, error) {
	lockKey := l.buildLockKey(key)
	exists, err := l.client.Exists(ctx, lockKey).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// GetLockValue 获取锁的值
func (l *DistLock) GetLockValue(ctx context.Context, key string) (string, error) {
	lockKey := l.buildLockKey(key)
	return l.client.Get(ctx, lockKey).Result()
}

// TryAcquireWithRetry 尝试获取锁，支持重试
func (l *DistLock) TryAcquireWithRetry(ctx context.Context, key string, value string, ttl time.Duration, maxRetries int, retryInterval time.Duration) (bool, error) {
	for i := 0; i <= maxRetries; i++ {
		acquired, err := l.Acquire(ctx, key, value, ttl)
		if err != nil {
			return false, err
		}
		if acquired {
			return true, nil
		}

		// 最后一次不需要等待
		if i < maxRetries {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(retryInterval):
				// 继续重试
			}
		}
	}
	return false, nil
}
