package cache

import (
	"context"
	"time"
)

// NoopSessionStore 空会话存储实现（用于不需要缓存的场景）
type NoopSessionStore struct{}

// NewNoopSessionStore 创建空会话存储
func NewNoopSessionStore() SessionStore {
	return &NoopSessionStore{}
}

func (n *NoopSessionStore) SaveSession(ctx context.Context, session *Session) error {
	return nil
}

func (n *NoopSessionStore) GetSession(ctx context.Context, jti string) (*Session, error) {
	return nil, ErrSessionNotFound
}

func (n *NoopSessionStore) DeleteSession(ctx context.Context, jti string) error {
	return nil
}

func (n *NoopSessionStore) DeleteAllSessions(ctx context.Context, subject string) error {
	return nil
}

func (n *NoopSessionStore) ListSessions(ctx context.Context, subject string) ([]*Session, error) {
	return nil, nil
}

// NoopBlacklist 空黑名单实现（用于不需要黑名单的场景）
type NoopBlacklist struct{}

// NewNoopBlacklist 创建空黑名单
func NewNoopBlacklist() Blacklist {
	return &NoopBlacklist{}
}

func (n *NoopBlacklist) Add(ctx context.Context, jti string, ttl time.Duration) error {
	return nil
}

func (n *NoopBlacklist) Contains(ctx context.Context, jti string) (bool, error) {
	return false, nil
}
