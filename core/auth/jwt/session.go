package jwt

import (
	"context"
	"time"
)

type SessionStatus string

const (
	SessionActive  SessionStatus = "active"
	SessionRevoked SessionStatus = "revoked"
)

type Session struct {
	ID         string        `json:"id"`
	Subject    string        `json:"subject"`
	DeviceID   string        `json:"device_id,omitempty"`
	RefreshJTI string        `json:"refresh_jti"`
	CreatedAt  time.Time     `json:"created_at"`
	LastSeenAt time.Time     `json:"last_seen_at"`
	ExpiresAt  time.Time     `json:"expires_at"`
	RevokedAt  *time.Time    `json:"revoked_at,omitempty"`
	Status     SessionStatus `json:"status"`
}

type SessionQuery struct {
	Status SessionStatus
	Limit  int
	Cursor string
}

type SessionPage struct {
	Sessions   []Session
	NextCursor string
}

type Rotation struct {
	Subject   string
	SessionID string
	OldJTI    string
	NewJTI    string
	Now       time.Time
	ExpiresAt time.Time
}

type SessionStore interface {
	Create(context.Context, Session, int) error
	Get(context.Context, string, string) (*Session, error)
	Rotate(context.Context, Rotation) error
	Revoke(context.Context, string, string, time.Time) error
	RevokeDevice(context.Context, string, string, time.Time) error
	RevokeAll(context.Context, string, time.Time) error
	List(context.Context, string, SessionQuery) (SessionPage, error)
}
