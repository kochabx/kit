package jwt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

type AuthConfig[T Claims] struct {
	ClaimsFactory                 ClaimsFactory[T]
	ClaimsLoader                  ClaimsLoader[T]
	KeyProvider                   KeyProvider
	Store                         SessionStore
	Clock                         Clock
	Issuer                        string
	AccessAudience                []string
	RefreshAudience               []string
	AccessTTL, RefreshTTL, Leeway time.Duration
	MaxSessions                   int
	RefreshLimiter                RefreshLimiter
	ClaimsLoadTimeout             time.Duration
	MaxTokenBytes                 int
}

type Auth[T Claims] struct {
	config AuthConfig[T]
	codec  *codec[T]
	loads  singleflight.Group
}

func New[T Claims](c AuthConfig[T]) (*Auth[T], error) {
	if !validAuthConfig(c) {
		return nil, ErrConfigInvalid
	}
	if c.Clock == nil {
		c.Clock = systemClock{}
	}
	if c.ClaimsLoadTimeout == 0 {
		c.ClaimsLoadTimeout = 3 * time.Second
	}
	if c.MaxTokenBytes == 0 {
		c.MaxTokenBytes = 8192
	}
	sample := c.ClaimsFactory()
	if isNil(sample) || sample.JWTClaims() == nil {
		return nil, ErrConfigInvalid
	}
	return &Auth[T]{
		config: c,
		codec: &codec[T]{
			keys:            c.KeyProvider,
			factory:         c.ClaimsFactory,
			issuer:          c.Issuer,
			accessAudience:  c.AccessAudience,
			refreshAudience: c.RefreshAudience,
			leeway:          c.Leeway,
			clock:           c.Clock,
			maxTokenBytes:   c.MaxTokenBytes,
		},
	}, nil
}

func validAuthConfig[T Claims](c AuthConfig[T]) bool {
	return c.ClaimsFactory != nil &&
		c.ClaimsLoader != nil &&
		c.KeyProvider != nil &&
		c.Store != nil &&
		c.Issuer != "" &&
		len(c.AccessAudience) > 0 &&
		len(c.RefreshAudience) > 0 &&
		audiencesDisjoint(c.AccessAudience, c.RefreshAudience) &&
		c.AccessTTL > 0 &&
		c.RefreshTTL > c.AccessTTL &&
		c.Leeway >= 0 &&
		c.MaxSessions > 0 && c.MaxSessions <= 100 &&
		c.ClaimsLoadTimeout >= 0 &&
		c.MaxTokenBytes >= 0
}

func audiencesDisjoint(left, right []string) bool {
	values := make(map[string]struct{}, len(left))
	for _, audience := range left {
		if audience == "" {
			return false
		}
		values[audience] = struct{}{}
	}
	for _, audience := range right {
		if audience == "" {
			return false
		}
		if _, exists := values[audience]; exists {
			return false
		}
	}
	return true
}

func (s *Auth[T]) Issue(ctx context.Context, claims T, opts ...IssueOption) (*TokenPair, error) {
	o := IssueOptions{}
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	if isNil(claims) || claims.JWTClaims() == nil {
		return nil, ErrInvalidClaims
	}
	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return nil, ErrInvalidClaims
	}
	now := s.config.Clock.Now()
	sid := uuid.NewString()
	pair, jti, err := s.issuePair(ctx, claims, subject, sid, now)
	if err != nil {
		return nil, err
	}
	session := Session{
		ID:         sid,
		Subject:    subject,
		DeviceID:   o.DeviceID,
		RefreshJTI: jti,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  pair.RefreshExpiresAt,
		Status:     SessionActive,
	}
	if err = s.config.Store.Create(ctx, session, s.config.MaxSessions); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return pair, nil
}

func (s *Auth[T]) Authenticate(ctx context.Context, raw string) (*Identity[T], error) {
	v, err := s.codec.verify(ctx, raw, accessToken)
	if err != nil {
		return nil, err
	}
	claims := v.claims.JWTClaims()
	rc := &claims.RegisteredClaims
	session, err := s.config.Store.Get(ctx, rc.Subject, claims.SessionID)
	if err != nil {
		return nil, fmt.Errorf("authenticate session: %w", err)
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(s.config.Clock.Now()) {
		return nil, ErrSessionRevoked
	}
	return &Identity[T]{
		Subject:   rc.Subject,
		SessionID: session.ID,
		TokenID:   rc.ID,
		DeviceID:  session.DeviceID,
		Claims:    v.claims,
		IssuedAt:  rc.IssuedAt.Time,
		ExpiresAt: rc.ExpiresAt.Time,
	}, nil
}

func (s *Auth[T]) Refresh(ctx context.Context, raw string) (*TokenPair, error) {
	v, err := s.codec.verify(ctx, raw, refreshToken)
	if err != nil {
		return nil, err
	}
	claims := v.claims.JWTClaims()
	rc := &claims.RegisteredClaims
	now := s.config.Clock.Now()
	if s.config.RefreshLimiter != nil {
		err := s.config.RefreshLimiter.Allow(ctx, rc.Subject, claims.SessionID)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRefreshRateLimited, err)
		}
	}
	resultCh := s.loads.DoChan(rc.Subject, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.ClaimsLoadTimeout)
		defer cancel()
		return s.config.ClaimsLoader(loadCtx, rc.Subject)
	})
	var result singleflight.Result
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result = <-resultCh:
	}
	if result.Err != nil {
		return nil, fmt.Errorf("load refresh claims: %w", result.Err)
	}
	fresh, ok := result.Val.(T)
	if !ok || isNil(fresh) || fresh.JWTClaims() == nil {
		return nil, ErrInvalidClaims
	}
	pair, jti, err := s.issuePair(ctx, fresh, rc.Subject, claims.SessionID, now)
	if err != nil {
		return nil, err
	}
	err = s.config.Store.Rotate(ctx, Rotation{
		Subject:   rc.Subject,
		SessionID: claims.SessionID,
		OldJTI:    rc.ID,
		NewJTI:    jti,
		Now:       now,
		ExpiresAt: pair.RefreshExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("rotate refresh: %w", err)
	}
	return pair, nil
}

func (s *Auth[T]) RevokeSession(ctx context.Context, sub, sid string) error {
	if sub == "" || sid == "" {
		return ErrInvalidSession
	}
	return s.config.Store.Revoke(ctx, sub, sid, s.config.Clock.Now())
}

func (s *Auth[T]) RevokeDevice(ctx context.Context, sub, deviceID string) error {
	if sub == "" || deviceID == "" {
		return ErrInvalidSession
	}
	return s.config.Store.RevokeDevice(ctx, sub, deviceID, s.config.Clock.Now())
}

func (s *Auth[T]) RevokeAll(ctx context.Context, sub string) error {
	if sub == "" {
		return ErrInvalidSession
	}
	return s.config.Store.RevokeAll(ctx, sub, s.config.Clock.Now())
}

func (s *Auth[T]) ListSessions(ctx context.Context, sub string, query SessionQuery) (SessionPage, error) {
	if sub == "" || !validSessionStatus(query.Status) {
		return SessionPage{}, ErrInvalidSession
	}
	return s.config.Store.List(ctx, sub, query)
}

func validSessionStatus(status SessionStatus) bool {
	return status == "" || status == SessionActive || status == SessionRevoked
}

func (s *Auth[T]) issuePair(
	ctx context.Context,
	src T,
	sub, sid string,
	now time.Time,
) (*TokenPair, string, error) {
	a, e := s.clone(src)
	if e != nil {
		return nil, "", e
	}
	r, e := s.clone(src)
	if e != nil {
		return nil, "", e
	}
	fill := func(c T, audience []string, ttl time.Duration) {
		claims := c.JWTClaims()
		rc := &claims.RegisteredClaims
		rc.ID = uuid.NewString()
		rc.Subject = sub
		rc.Issuer = s.config.Issuer
		rc.Audience = append(gojwt.ClaimStrings(nil), audience...)
		rc.IssuedAt = gojwt.NewNumericDate(now)
		rc.NotBefore = gojwt.NewNumericDate(now)
		rc.ExpiresAt = gojwt.NewNumericDate(now.Add(ttl))
		claims.SessionID = sid
	}
	fill(a, s.config.AccessAudience, s.config.AccessTTL)
	fill(r, s.config.RefreshAudience, s.config.RefreshTTL)
	at, e := s.codec.sign(ctx, a)
	if e != nil {
		return nil, "", e
	}
	rt, e := s.codec.sign(ctx, r)
	if e != nil {
		return nil, "", e
	}
	return &TokenPair{
		AccessToken:      at,
		RefreshToken:     rt,
		AccessExpiresAt:  now.Add(s.config.AccessTTL),
		RefreshExpiresAt: now.Add(s.config.RefreshTTL),
	}, r.JWTClaims().ID, nil
}

func (s *Auth[T]) clone(src T) (T, error) {
	dst := s.config.ClaimsFactory()
	if isNil(dst) || dst.JWTClaims() == nil {
		var zero T
		return zero, ErrInvalidClaims
	}
	b, e := json.Marshal(src)
	if e == nil {
		e = json.Unmarshal(b, dst)
	}
	if e != nil {
		var z T
		return z, fmt.Errorf("clone claims: %w", e)
	}
	return dst, nil
}
