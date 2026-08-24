package jwt

import (
	"context"
	"time"
)

type ClaimsFactory[T Claims] func() T

type ClaimsLoader[T Claims] func(context.Context, string) (T, error)

type RefreshLimiter interface {
	Allow(context.Context, string, string) error
}

type Identity[T Claims] struct {
	Subject   string
	SessionID string
	TokenID   string
	DeviceID  string
	Claims    T
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type Issuer[T Claims] interface {
	Issue(context.Context, T, ...IssueOption) (*TokenPair, error)
}

type Authenticator[T Claims] interface {
	Authenticate(context.Context, string) (*Identity[T], error)
}

type Refresher interface {
	Refresh(context.Context, string) (*TokenPair, error)
}

type SessionController interface {
	RevokeSession(context.Context, string, string) error
	RevokeDevice(context.Context, string, string) error
	RevokeAll(context.Context, string) error
	ListSessions(context.Context, string, SessionQuery) (SessionPage, error)
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type IssueOptions struct {
	DeviceID string
}

type IssueOption func(*IssueOptions)

func WithIssueDeviceID(id string) IssueOption {
	return func(options *IssueOptions) {
		options.DeviceID = id
	}
}
