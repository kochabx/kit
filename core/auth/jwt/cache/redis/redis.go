package redis

import (
	"github.com/kochabx/kit/core/auth/jwt/cache"
	"github.com/redis/go-redis/v9"
)

// Store Redis 存储（同时包含 SessionStore 和 Blacklist）
type Store struct {
	*SessionStore
	*Blacklist
}

// StoreOption Store 选项
type StoreOption func(*Store)

// WithStoreKeyPrefix 设置所有 key 的统一前缀
func WithStoreKeyPrefix(prefix string) StoreOption {
	return func(s *Store) {
		s.SessionStore.keyPrefix = prefix + ":session:"
		s.SessionStore.subjectIndex = prefix + ":subject:"
		s.Blacklist.keyPrefix = prefix + ":blacklist:"
	}
}

// NewStore 创建包含 SessionStore 和 Blacklist 的 Redis 存储
func NewStore(client *redis.Client, opts ...StoreOption) *Store {
	store := &Store{
		SessionStore: NewSessionStore(client),
		Blacklist:    NewBlacklist(client),
	}

	for _, opt := range opts {
		opt(store)
	}

	return store
}

// 确保实现接口
var (
	_ cache.SessionStore = (*SessionStore)(nil)
	_ cache.Blacklist    = (*Blacklist)(nil)
)
