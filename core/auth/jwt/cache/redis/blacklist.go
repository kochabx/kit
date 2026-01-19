package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Blacklist Redis 黑名单实现
type Blacklist struct {
	client    *redis.Client
	keyPrefix string // "jwt:blacklist:"
}

// BlacklistOption 黑名单选项
type BlacklistOption func(*Blacklist)

// WithBlacklistKeyPrefix 设置黑名单 key 前缀
func WithBlacklistKeyPrefix(prefix string) BlacklistOption {
	return func(b *Blacklist) {
		b.keyPrefix = prefix
	}
}

// NewBlacklist 创建 Redis 黑名单
func NewBlacklist(client *redis.Client, opts ...BlacklistOption) *Blacklist {
	bl := &Blacklist{
		client:    client,
		keyPrefix: "jwt:blacklist:",
	}

	for _, opt := range opts {
		opt(bl)
	}

	return bl
}

// Add 添加 token 到黑名单
func (b *Blacklist) Add(ctx context.Context, jti string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil // 已过期的 token 不需要加入黑名单
	}

	key := b.keyPrefix + jti
	return b.client.Set(ctx, key, "1", ttl).Err()
}

// Contains 检查 token 是否在黑名单中
func (b *Blacklist) Contains(ctx context.Context, jti string) (bool, error) {
	key := b.keyPrefix + jti
	exists, err := b.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}
