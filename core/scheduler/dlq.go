package scheduler

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// DeadLetterQueue 死信队列
type DeadLetterQueue struct {
	client    *redis.Client
	namespace string
	enabled   bool
	maxSize   int
}

// NewDeadLetterQueue 创建死信队列
func NewDeadLetterQueue(client *redis.Client, namespace string, enabled bool, maxSize int) *DeadLetterQueue {
	return &DeadLetterQueue{
		client:    client,
		namespace: namespace,
		enabled:   enabled,
		maxSize:   maxSize,
	}
}

// keyDLQ 死信队列key
func (d *DeadLetterQueue) keyDLQ() string {
	return d.namespace + ":dlq"
}

// Add 添加任务到死信队列
func (d *DeadLetterQueue) Add(ctx context.Context, taskID string) error {
	if !d.enabled {
		return nil
	}

	key := d.keyDLQ()

	// 使用LPUSH添加到队列头部
	if err := d.client.LPush(ctx, key, taskID).Err(); err != nil {
		return err
	}

	// 如果设置了最大容量，则裁剪队列
	if d.maxSize > 0 {
		// 保留前maxSize个元素
		return d.client.LTrim(ctx, key, 0, int64(d.maxSize-1)).Err()
	}

	return nil
}

// Get 获取死信队列中的任务（不移除）
func (d *DeadLetterQueue) Get(ctx context.Context, start, stop int64) ([]string, error) {
	if !d.enabled {
		return nil, nil
	}

	key := d.keyDLQ()
	return d.client.LRange(ctx, key, start, stop).Result()
}

// GetAll 获取所有死信任务
func (d *DeadLetterQueue) GetAll(ctx context.Context) ([]string, error) {
	return d.Get(ctx, 0, -1)
}

// Remove 从死信队列移除指定任务
func (d *DeadLetterQueue) Remove(ctx context.Context, taskID string) error {
	if !d.enabled {
		return nil
	}

	key := d.keyDLQ()
	// LREM count=0 表示移除所有匹配的元素
	return d.client.LRem(ctx, key, 0, taskID).Err()
}

// Pop 从死信队列弹出一个任务
func (d *DeadLetterQueue) Pop(ctx context.Context) (string, error) {
	if !d.enabled {
		return "", nil
	}

	key := d.keyDLQ()
	result, err := d.client.RPop(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return result, err
}

// Count 获取死信队列中的任务数量
func (d *DeadLetterQueue) Count(ctx context.Context) (int64, error) {
	if !d.enabled {
		return 0, nil
	}

	key := d.keyDLQ()
	return d.client.LLen(ctx, key).Result()
}

// Clear 清空死信队列
func (d *DeadLetterQueue) Clear(ctx context.Context) error {
	if !d.enabled {
		return nil
	}

	key := d.keyDLQ()
	return d.client.Del(ctx, key).Err()
}

// IsFull 检查死信队列是否已满
func (d *DeadLetterQueue) IsFull(ctx context.Context) (bool, error) {
	if !d.enabled || d.maxSize <= 0 {
		return false, nil
	}

	count, err := d.Count(ctx)
	if err != nil {
		return false, err
	}

	return count >= int64(d.maxSize), nil
}

// BatchRemove 批量移除任务
func (d *DeadLetterQueue) BatchRemove(ctx context.Context, taskIDs []string) error {
	if !d.enabled || len(taskIDs) == 0 {
		return nil
	}

	key := d.keyDLQ()
	pipe := d.client.Pipeline()

	for _, taskID := range taskIDs {
		pipe.LRem(ctx, key, 0, taskID)
	}

	_, err := pipe.Exec(ctx)
	return err
}
