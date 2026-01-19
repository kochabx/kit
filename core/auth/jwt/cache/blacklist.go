package cache

import (
	"context"
	"time"
)

// Blacklist Token 黑名单接口
type Blacklist interface {
	// Add 添加 token 到黑名单
	Add(ctx context.Context, jti string, ttl time.Duration) error

	// Contains 检查 token 是否在黑名单中
	Contains(ctx context.Context, jti string) (bool, error)
}
