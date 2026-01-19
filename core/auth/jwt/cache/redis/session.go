package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/auth/jwt/cache"
	kitredis "github.com/kochabx/kit/store/redis"
)

// SessionStore Redis 会话存储实现
type SessionStore struct {
	client       *kitredis.Client
	keyPrefix    string // "jwt:session:"
	subjectIndex string // "jwt:subject:"
}

// SessionStoreOption 会话存储选项
type SessionStoreOption func(*SessionStore)

// WithSessionKeyPrefix 设置会话 key 前缀
func WithSessionKeyPrefix(prefix string) SessionStoreOption {
	return func(s *SessionStore) {
		s.keyPrefix = prefix
	}
}

// WithSubjectIndexPrefix 设置主体索引前缀
func WithSubjectIndexPrefix(prefix string) SessionStoreOption {
	return func(s *SessionStore) {
		s.subjectIndex = prefix
	}
}

// NewSessionStore 创建 Redis 会话存储
func NewSessionStore(client *kitredis.Client, opts ...SessionStoreOption) *SessionStore {
	store := &SessionStore{
		client:       client,
		keyPrefix:    "jwt:session:",
		subjectIndex: "jwt:subject:",
	}

	for _, opt := range opts {
		opt(store)
	}

	return store
}

// SaveSession 保存会话
func (s *SessionStore) SaveSession(ctx context.Context, session *cache.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}

	// 序列化会话数据
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	// 计算 TTL
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session already expired")
	}

	pipe := s.client.UniversalClient().Pipeline()

	// 1. 保存会话数据 (key: jwt:session:{jti})
	sessionKey := s.keyPrefix + session.JTI
	pipe.Set(ctx, sessionKey, data, ttl)

	// 2. 添加到主体索引 (key: jwt:subject:{subject}, value: set of jti)
	subjectKey := s.subjectIndex + session.Subject
	pipe.SAdd(ctx, subjectKey, session.JTI)
	pipe.Expire(ctx, subjectKey, ttl)

	_, err = pipe.Exec(ctx)
	return err
}

// GetSession 获取会话
func (s *SessionStore) GetSession(ctx context.Context, jti string) (*cache.Session, error) {
	sessionKey := s.keyPrefix + jti

	data, err := s.client.UniversalClient().Get(ctx, sessionKey).Bytes()
	if err == kitredis.ErrNil {
		return nil, cache.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	var session cache.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	return &session, nil
}

// DeleteSession 删除会话
func (s *SessionStore) DeleteSession(ctx context.Context, jti string) error {
	// 先获取会话以获得 subject
	session, err := s.GetSession(ctx, jti)
	if err != nil {
		if err == cache.ErrSessionNotFound {
			return nil // 已经不存在
		}
		return err
	}

	pipe := s.client.UniversalClient().Pipeline()

	// 1. 删除会话数据
	sessionKey := s.keyPrefix + jti
	pipe.Del(ctx, sessionKey)

	// 2. 从主体索引中移除
	subjectKey := s.subjectIndex + session.Subject
	pipe.SRem(ctx, subjectKey, jti)

	_, err = pipe.Exec(ctx)
	return err
}

// DeleteAllSessions 删除用户所有会话
func (s *SessionStore) DeleteAllSessions(ctx context.Context, subject string) error {
	subjectKey := s.subjectIndex + subject

	// 获取所有 JTI
	jtis, err := s.client.UniversalClient().SMembers(ctx, subjectKey).Result()
	if err != nil {
		return fmt.Errorf("get subject jtis: %w", err)
	}

	if len(jtis) == 0 {
		return nil
	}

	pipe := s.client.UniversalClient().Pipeline()

	// 删除所有会话
	for _, jti := range jtis {
		sessionKey := s.keyPrefix + jti
		pipe.Del(ctx, sessionKey)
	}

	// 删除主体索引
	pipe.Del(ctx, subjectKey)

	_, err = pipe.Exec(ctx)
	return err
}

// ListSessions 列出用户所有会话
func (s *SessionStore) ListSessions(ctx context.Context, subject string) ([]*cache.Session, error) {
	subjectKey := s.subjectIndex + subject

	// 获取所有 JTI
	jtis, err := s.client.UniversalClient().SMembers(ctx, subjectKey).Result()
	if err != nil {
		return nil, fmt.Errorf("get subject jtis: %w", err)
	}

	if len(jtis) == 0 {
		return []*cache.Session{}, nil
	}

	// 批量获取会话
	sessions := make([]*cache.Session, 0, len(jtis))
	for _, jti := range jtis {
		session, err := s.GetSession(ctx, jti)
		if err != nil {
			if err == cache.ErrSessionNotFound {
				// 会话已过期或被删除，从索引中清理
				s.client.UniversalClient().SRem(ctx, subjectKey, jti)
				continue
			}
			return nil, fmt.Errorf("get session %s: %w", jti, err)
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}
