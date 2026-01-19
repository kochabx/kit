package cache

import (
	"context"
	"time"
)

// Session 会话信息
type Session struct {
	JTI       string    `json:"jti"`
	Subject   string    `json:"subject"`
	TokenType string    `json:"token_type"` // "access" or "refresh"
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	DeviceID  string    `json:"device_id,omitempty"`
}

// SessionStore 会话存储接口
type SessionStore interface {
	// SaveSession 保存会话
	SaveSession(ctx context.Context, session *Session) error

	// GetSession 获取会话
	GetSession(ctx context.Context, jti string) (*Session, error)

	// DeleteSession 删除会话
	DeleteSession(ctx context.Context, jti string) error

	// DeleteAllSessions 删除用户所有会话
	DeleteAllSessions(ctx context.Context, subject string) error

	// ListSessions 列出用户所有会话
	ListSessions(ctx context.Context, subject string) ([]*Session, error)
}
