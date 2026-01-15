package scheduler

import (
	"context"
	"time"
)

// LockProvider 分布式锁接口
type LockProvider interface {
	// Acquire 获取锁
	Acquire(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)

	// Release 释放锁
	Release(ctx context.Context, key string, value string) (bool, error)

	// Extend 延长锁的TTL
	Extend(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)

	// IsLocked 检查锁是否存在
	IsLocked(ctx context.Context, key string) (bool, error)

	// GetLockValue 获取锁的值
	GetLockValue(ctx context.Context, key string) (string, error)
}

// QueueStore 队列存储接口
type QueueStore interface {
	// SetConsumer 设置消费者名称
	SetConsumer(consumerName string)

	// AddDelayed 添加任务到延迟队列
	AddDelayed(ctx context.Context, taskID string, score float64) error

	// AddReady 添加任务到就绪队列
	AddReady(ctx context.Context, taskID string, priority Priority) error

	// PopReady 从就绪队列获取任务
	PopReady(ctx context.Context, timeout int) (taskID string, priority Priority, msgID string, err error)

	// AckMessage 确认消息已处理
	AckMessage(ctx context.Context, priority Priority, msgID string) error

	// MoveDelayedToReady 移动到期任务到就绪队列
	MoveDelayedToReady(ctx context.Context, now int64, batchSize int) (int64, error)

	// ClaimStaleMessages 接管超时的Pending消息
	ClaimStaleMessages(ctx context.Context, priority Priority, idleTime time.Duration) ([]string, error)

	// RemoveDelayed 从延迟队列移除任务
	RemoveDelayed(ctx context.Context, taskID string) error

	// RemoveReady 从就绪队列移除任务
	RemoveReady(ctx context.Context, taskID string) error

	// GetStats 获取队列统计信息
	GetStats(ctx context.Context) (*QueueStats, error)
}

// DeduplicationStore 去重存储接口
type DeduplicationStore interface {
	// Check 检查任务是否重复
	Check(ctx context.Context, dedupKey string) (bool, string, error)

	// Set 设置去重记录
	Set(ctx context.Context, dedupKey string, taskID string, ttl time.Duration) error

	// SetNX 设置去重记录（仅当不存在时）
	SetNX(ctx context.Context, dedupKey string, taskID string, ttl time.Duration) (bool, error)

	// Delete 删除去重记录
	Delete(ctx context.Context, dedupKey string) error

	// Extend 延长去重记录的TTL
	Extend(ctx context.Context, dedupKey string, ttl time.Duration) error
}

// DeadLetterStore 死信队列存储接口
type DeadLetterStore interface {
	// Add 添加任务到死信队列
	Add(ctx context.Context, taskID string) error

	// Get 获取死信队列中的任务（不移除）
	Get(ctx context.Context, start, stop int64) ([]string, error)

	// GetAll 获取所有死信任务
	GetAll(ctx context.Context) ([]string, error)

	// Remove 从死信队列移除指定任务
	Remove(ctx context.Context, taskID string) error

	// Pop 从死信队列弹出一个任务
	Pop(ctx context.Context) (string, error)

	// Count 获取死信队列中的任务数量
	Count(ctx context.Context) (int64, error)

	// Clear 清空死信队列
	Clear(ctx context.Context) error
}
