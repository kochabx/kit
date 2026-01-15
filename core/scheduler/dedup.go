package scheduler

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Deduplicator 任务去重器
type Deduplicator struct {
	client     *redis.Client
	namespace  string
	enabled    bool
	defaultTTL time.Duration
}

// NewDeduplicator 创建去重器
func NewDeduplicator(client *redis.Client, namespace string, enabled bool, defaultTTL time.Duration) *Deduplicator {
	return &Deduplicator{
		client:     client,
		namespace:  namespace,
		enabled:    enabled,
		defaultTTL: defaultTTL,
	}
}

// keyDedup 去重key
func (d *Deduplicator) keyDedup(dedupKey string) string {
	return d.namespace + ":dedup:" + dedupKey
}

// Check 检查任务是否重复
// 返回: (是否重复, 已存在的任务ID, error)
func (d *Deduplicator) Check(ctx context.Context, dedupKey string) (bool, string, error) {
	if !d.enabled || dedupKey == "" {
		return false, "", nil
	}

	key := d.keyDedup(dedupKey)
	taskID, err := d.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, "", nil // 不重复
	}
	if err != nil {
		return false, "", err
	}

	return true, taskID, nil
}

// Set 设置去重记录
func (d *Deduplicator) Set(ctx context.Context, dedupKey string, taskID string, ttl time.Duration) error {
	if !d.enabled || dedupKey == "" {
		return nil
	}

	if ttl == 0 {
		ttl = d.defaultTTL
	}

	key := d.keyDedup(dedupKey)
	return d.client.Set(ctx, key, taskID, ttl).Err()
}

// SetNX 设置去重记录（仅当不存在时）
// 返回: (是否设置成功, error)
func (d *Deduplicator) SetNX(ctx context.Context, dedupKey string, taskID string, ttl time.Duration) (bool, error) {
	if !d.enabled || dedupKey == "" {
		return true, nil
	}

	if ttl == 0 {
		ttl = d.defaultTTL
	}

	key := d.keyDedup(dedupKey)
	result, err := d.client.SetNX(ctx, key, taskID, ttl).Result()
	return result, err
}

// Delete 删除去重记录
func (d *Deduplicator) Delete(ctx context.Context, dedupKey string) error {
	if !d.enabled || dedupKey == "" {
		return nil
	}

	key := d.keyDedup(dedupKey)
	return d.client.Del(ctx, key).Err()
}

// Extend 延长去重记录的TTL
func (d *Deduplicator) Extend(ctx context.Context, dedupKey string, ttl time.Duration) error {
	if !d.enabled || dedupKey == "" {
		return nil
	}

	key := d.keyDedup(dedupKey)
	return d.client.Expire(ctx, key, ttl).Err()
}

// GetTaskID 获取去重键对应的任务ID
func (d *Deduplicator) GetTaskID(ctx context.Context, dedupKey string) (string, error) {
	if !d.enabled || dedupKey == "" {
		return "", nil
	}

	key := d.keyDedup(dedupKey)
	taskID, err := d.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return taskID, err
}

// GetTTL 获取去重键的剩余TTL
func (d *Deduplicator) GetTTL(ctx context.Context, dedupKey string) (time.Duration, error) {
	if !d.enabled || dedupKey == "" {
		return 0, nil
	}

	key := d.keyDedup(dedupKey)
	return d.client.TTL(ctx, key).Result()
}
