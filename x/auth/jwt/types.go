package jwt

import (
	"context"
	"iter"
	"time"
)

// TokenKind identifies whether a JWT is an access token or a refresh token.
type TokenKind string

const (
	TokenKindAccess  TokenKind = "access"
	TokenKindRefresh TokenKind = "refresh"
)

// Session status constants persisted in SessionStore.
const (
	SessionActive  = "active"
	SessionRevoked = "revoked"
)

// Session represents a stateful refresh session tracked by SessionStore.
// Timestamps are stored in Unix milliseconds to align directly with Redis PXAT/PEXPIREAT.
type Session struct {
	ID           string            `json:"id"`
	Subject      string            `json:"subject"`
	DeviceID     string            `json:"device_id,omitzero"`
	DeviceKey    string            `json:"device_key,omitzero"`
	RefreshJTI   string            `json:"refresh_jti"`
	Status       string            `json:"status"`
	IssuedAtMs   int64             `json:"issued_at_ms"`
	LastSeenAtMs int64             `json:"last_seen_at_ms"`
	ExpiresAtMs  int64             `json:"expires_at_ms"`
	RevokedAtMs  *int64            `json:"revoked_at_ms,omitzero"`
	Metadata     map[string]string `json:"metadata,omitzero"`
}

// IsRevokedAt reports whether the session is revoked or expired as of now.
func (s Session) IsRevokedAt(now time.Time) bool {
	if s.RevokedAtMs != nil || s.Status == SessionRevoked {
		return true
	}
	return s.ExpiresAtMs <= now.UnixMilli()
}

// Rotation carries parameters for an atomic refresh-token rotation.
type Rotation struct {
	Subject       string
	SessionID     string
	OldRefreshJTI string
	NewRefreshJTI string
	Now           time.Time
	ExpiresAt     time.Time
}

// TokenPair is returned by Issue and Refresh.
type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	SessionID        string    `json:"session_id"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// Claims holds verified JWT fields extracted from a token.
type Claims struct {
	Subject   string         `json:"subject"`
	Issuer    string         `json:"issuer,omitzero"`
	Audience  []string       `json:"audience,omitzero"`
	JTI       string         `json:"jti"`
	SessionID string         `json:"session_id"`
	Kind      TokenKind      `json:"kind"`
	IssuedAt  time.Time      `json:"issued_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	Extra     map[string]any `json:"extra,omitzero"`
}

// ClaimsLoader enriches verified Claims only after signature and revocation checks succeed.
type ClaimsLoader func(ctx context.Context, claims *Claims) error

// SessionStore persists and revokes stateful refresh sessions.
type SessionStore interface {
	Create(ctx context.Context, session Session, maxPerSubject int) error
	Get(ctx context.Context, subject, sessionID string) (Session, error)
	Rotate(ctx context.Context, r Rotation) (Session, error)
	Revoke(ctx context.Context, subject, sessionID string, at time.Time) error
	RevokeDevice(ctx context.Context, subject, deviceID string, at time.Time) error
	RevokeAll(ctx context.Context, subject string, at time.Time) error
	IsRevoked(ctx context.Context, subject, jti, sessionID string) (bool, error)
	ListSessions(ctx context.Context, subject string) iter.Seq2[Session, error]
}
