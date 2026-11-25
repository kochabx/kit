package jwt

import (
	"cmp"
	"context"
	"errors"
	"iter"
	"log/slog"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	jwxjwt "github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/kochabx/kit/fault"
)

const (
	defaultAccessTTL   = 15 * time.Minute
	defaultRefreshTTL  = 7 * 24 * time.Hour
	defaultLeeway      = 30 * time.Second
	defaultMaxSessions = 10

	claimSessionID = "sid"
	claimKind      = "typ"
	claimExtra     = "extra"
)

var (
	// ErrSessionNotFound indicates the target session does not exist or has expired.
	ErrSessionNotFound = fault.Unauthorized("session not found")
	// ErrSessionRevoked indicates the target session has been revoked.
	ErrSessionRevoked = fault.Unauthorized("session revoked")
	// ErrRefreshReused indicates a previously rotated refresh token was replayed.
	ErrRefreshReused = fault.Unauthorized("refresh token reuse detected")
)

// Config configures a JWT Service.
type Config struct {
	Issuer                string           `json:"issuer"`
	Audience              []string         `json:"audience"`
	AccessTTL             time.Duration    `json:"access_ttl"`
	RefreshTTL            time.Duration    `json:"refresh_ttl"`
	Leeway                time.Duration    `json:"leeway"`
	MaxSessionsPerSubject int              `json:"max_sessions_per_subject"`
	KeyRing               *KeyRing         `json:"-"`
	Clock                 func() time.Time `json:"-"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if c.KeyRing == nil {
		errs = append(errs, errors.New("jwt: keyring is required"))
	}
	if c.AccessTTL < 0 {
		errs = append(errs, errors.New("jwt: access_ttl cannot be negative"))
	}
	if c.RefreshTTL < 0 {
		errs = append(errs, errors.New("jwt: refresh_ttl cannot be negative"))
	}
	if c.Leeway < 0 {
		errs = append(errs, errors.New("jwt: leeway cannot be negative"))
	}
	if c.MaxSessionsPerSubject < 0 {
		errs = append(errs, errors.New("jwt: max_sessions_per_subject cannot be negative"))
	}
	return errors.Join(errs...)
}

// Option configures a Service.
type Option func(*options)

type options struct {
	logger       *slog.Logger
	store        SessionStore
	claimsLoader ClaimsLoader
}

// WithLogger injects a structured logger. Defaults to a discard logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithSessionStore attaches a stateful SessionStore for refresh rotation and revocation checks.
func WithSessionStore(store SessionStore) Option {
	return func(o *options) {
		o.store = store
	}
}

// WithClaimsLoader registers a callback invoked ONLY after signature and revocation checks pass.
func WithClaimsLoader(loader ClaimsLoader) Option {
	return func(o *options) {
		o.claimsLoader = loader
	}
}

// Service issues, verifies, refreshes, and revokes JWT tokens and sessions.
type Service struct {
	issuer       string
	audience     []string
	accessTTL    time.Duration
	refreshTTL   time.Duration
	leeway       time.Duration
	maxSessions  int
	ring         *KeyRing
	clock        func() time.Time
	logger       *slog.Logger
	store        SessionStore
	claimsLoader ClaimsLoader
}

// New constructs a JWT Service.
func New(cfg Config, opts ...Option) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fault.InvalidArgument("invalid jwt config").WithCause(err)
	}
	if _, _, err := cfg.KeyRing.signingKey(); err != nil {
		return nil, fault.InvalidArgument("invalid jwt keyring").WithCause(err)
	}

	o := options{
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	return &Service{
		issuer:       cfg.Issuer,
		audience:     slices.Clone(cfg.Audience),
		accessTTL:    cmp.Or(cfg.AccessTTL, defaultAccessTTL),
		refreshTTL:   cmp.Or(cfg.RefreshTTL, defaultRefreshTTL),
		leeway:       cmp.Or(cfg.Leeway, defaultLeeway),
		maxSessions:  cmp.Or(cfg.MaxSessionsPerSubject, defaultMaxSessions),
		ring:         cfg.KeyRing,
		clock:        clock,
		logger:       o.logger,
		store:        o.store,
		claimsLoader: o.claimsLoader,
	}, nil
}

// Issue generates an access/refresh TokenPair for subject and persists the session when configured.
func (s *Service) Issue(ctx context.Context, subject, deviceID string, extra map[string]any) (TokenPair, error) {
	if subject == "" {
		return TokenPair{}, fault.InvalidArgument("subject is required")
	}

	now := s.clock().UTC()
	sessionID := uuid.NewV7().String()
	accessJTI := uuid.NewV7().String()
	refreshJTI := uuid.NewV7().String()
	accessExp := now.Add(s.accessTTL)
	refreshExp := now.Add(s.refreshTTL)

	accessTok, err := s.signToken(subject, sessionID, accessJTI, TokenKindAccess, now, accessExp, extra)
	if err != nil {
		return TokenPair{}, err
	}
	refreshTok, err := s.signToken(subject, sessionID, refreshJTI, TokenKindRefresh, now, refreshExp, nil)
	if err != nil {
		return TokenPair{}, err
	}

	if s.store != nil {
		session := Session{
			ID:           sessionID,
			Subject:      subject,
			DeviceID:     deviceID,
			RefreshJTI:   refreshJTI,
			Status:       SessionActive,
			IssuedAtMs:   now.UnixMilli(),
			LastSeenAtMs: now.UnixMilli(),
			ExpiresAtMs:  refreshExp.UnixMilli(),
		}
		if err := s.store.Create(ctx, session, s.maxSessions); err != nil {
			if f, ok := fault.From(err); ok {
				return TokenPair{}, f
			}
			return TokenPair{}, fault.Internal("failed to persist session").WithCause(err)
		}
	}

	return TokenPair{
		AccessToken:      accessTok,
		RefreshToken:     refreshTok,
		SessionID:        sessionID,
		AccessExpiresAt:  accessExp,
		RefreshExpiresAt: refreshExp,
	}, nil
}

// Verify validates signature, standard claims, token kind, revocation status, and then runs ClaimsLoader.
func (s *Service) Verify(ctx context.Context, tokenStr string, expectedKind TokenKind) (*Claims, error) {
	if tokenStr == "" {
		return nil, fault.Unauthorized("missing token")
	}
	verifySet, err := s.ring.verifySet()
	if err != nil {
		return nil, fault.Internal("failed to load verification keys").WithCause(err)
	}

	parseOpts := []jwxjwt.ParseOption{
		jwxjwt.WithKeySet(verifySet, jws.WithRequireKid(true)),
		jwxjwt.WithClock(jwxjwt.ClockFunc(s.clock)),
		jwxjwt.WithAcceptableSkew(s.leeway),
		jwxjwt.WithRequiredClaim(jwxjwt.ExpirationKey),
	}
	if s.issuer != "" {
		parseOpts = append(parseOpts, jwxjwt.WithIssuer(s.issuer))
	}
	for _, aud := range s.audience {
		parseOpts = append(parseOpts, jwxjwt.WithAudience(aud))
	}

	tok, err := jwxjwt.ParseString(tokenStr, parseOpts...)
	if err != nil {
		return nil, fault.Unauthorized("invalid or expired token").WithCause(err)
	}

	subject, ok := tok.Subject()
	if !ok || subject == "" {
		return nil, fault.Unauthorized("token missing subject")
	}
	jti, ok := tok.JwtID()
	if !ok || jti == "" {
		return nil, fault.Unauthorized("token missing jti")
	}

	var rawKind string
	if err := tok.Get(claimKind, &rawKind); err != nil || TokenKind(rawKind) != expectedKind {
		return nil, fault.Unauthorized("unexpected token type")
	}
	var sessionID string
	if err := tok.Get(claimSessionID, &sessionID); err != nil || sessionID == "" {
		return nil, fault.Unauthorized("token missing session id")
	}

	// Check JTI/session revocation BEFORE calling ClaimsLoader so revoked tokens never hit DB loaders.
	if s.store != nil {
		revoked, err := s.store.IsRevoked(ctx, subject, jti, sessionID)
		if err != nil {
			if f, ok := fault.From(err); ok {
				return nil, f
			}
			return nil, fault.Internal("failed to check session revocation").WithCause(err)
		}
		if revoked {
			return nil, ErrSessionRevoked
		}
	}

	iss, _ := tok.Issuer()
	aud, _ := tok.Audience()
	iat, _ := tok.IssuedAt()
	exp, _ := tok.Expiration()

	var extra map[string]any
	if err := tok.Get(claimExtra, &extra); err == nil && extra != nil {
		extra = maps.Clone(extra)
	}

	claims := &Claims{
		Subject:   subject,
		Issuer:    iss,
		Audience:  aud,
		JTI:       jti,
		SessionID: sessionID,
		Kind:      TokenKind(rawKind),
		IssuedAt:  iat,
		ExpiresAt: exp,
		Extra:     extra,
	}

	if s.claimsLoader != nil {
		if err := s.claimsLoader(ctx, claims); err != nil {
			if f, ok := errors.AsType[*fault.Error](err); ok {
				return nil, f
			}
			return nil, fault.Unauthorized("claims loader rejected token").WithCause(err)
		}
	}
	return claims, nil
}

// Refresh verifies refreshToken, atomically rotates the refresh JTI in SessionStore, and signs a new TokenPair.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	claims, err := s.Verify(ctx, refreshToken, TokenKindRefresh)
	if err != nil {
		return TokenPair{}, err
	}

	now := s.clock().UTC()
	accessJTI := uuid.NewV7().String()
	newRefreshJTI := uuid.NewV7().String()
	accessExp := now.Add(s.accessTTL)
	refreshExp := now.Add(s.refreshTTL)

	accessTok, err := s.signToken(claims.Subject, claims.SessionID, accessJTI, TokenKindAccess, now, accessExp, claims.Extra)
	if err != nil {
		return TokenPair{}, err
	}
	newRefreshTok, err := s.signToken(claims.Subject, claims.SessionID, newRefreshJTI, TokenKindRefresh, now, refreshExp, nil)
	if err != nil {
		return TokenPair{}, err
	}

	if s.store != nil {
		if _, err := s.store.Rotate(ctx, Rotation{
			Subject:       claims.Subject,
			SessionID:     claims.SessionID,
			OldRefreshJTI: claims.JTI,
			NewRefreshJTI: newRefreshJTI,
			Now:           now,
			ExpiresAt:     refreshExp,
		}); err != nil {
			if errors.Is(err, ErrRefreshReused) {
				s.logger.WarnContext(ctx, "refresh token reuse detected",
					slog.String("subject", claims.Subject),
					slog.String("session_id", claims.SessionID),
				)
			}
			if f, ok := fault.From(err); ok {
				return TokenPair{}, f
			}
			return TokenPair{}, fault.Unauthorized("refresh session failed").WithCause(err)
		}
	}

	return TokenPair{
		AccessToken:      accessTok,
		RefreshToken:     newRefreshTok,
		SessionID:        claims.SessionID,
		AccessExpiresAt:  accessExp,
		RefreshExpiresAt: refreshExp,
	}, nil
}

// Revoke revokes a single session for subject.
func (s *Service) Revoke(ctx context.Context, subject, sessionID string) error {
	if s.store == nil {
		return fault.InvalidArgument("session store not configured")
	}
	return s.store.Revoke(ctx, subject, sessionID, s.clock().UTC())
}

// RevokeDevice revokes the active session bound to deviceID for subject.
func (s *Service) RevokeDevice(ctx context.Context, subject, deviceID string) error {
	if s.store == nil {
		return fault.InvalidArgument("session store not configured")
	}
	return s.store.RevokeDevice(ctx, subject, deviceID, s.clock().UTC())
}

// RevokeAll revokes every session belonging to subject.
func (s *Service) RevokeAll(ctx context.Context, subject string) error {
	if s.store == nil {
		return fault.InvalidArgument("session store not configured")
	}
	return s.store.RevokeAll(ctx, subject, s.clock().UTC())
}

// ListSessions streams all sessions belonging to subject using Go iter.Seq2.
func (s *Service) ListSessions(ctx context.Context, subject string) iter.Seq2[Session, error] {
	if s.store == nil {
		return func(yield func(Session, error) bool) {
			yield(Session{}, fault.InvalidArgument("session store not configured"))
		}
	}
	return s.store.ListSessions(ctx, subject)
}

// JWKS returns the public JSON Web Key Set suitable for publishing at /.well-known/jwks.json.
func (s *Service) JWKS() (jwk.Set, error) {
	return s.ring.PublicJWKS()
}

func (s *Service) signToken(
	subject, sessionID, jti string,
	kind TokenKind,
	issuedAt, expiresAt time.Time,
	extra map[string]any,
) (string, error) {
	key, alg, err := s.ring.signingKey()
	if err != nil {
		return "", fault.Internal("failed to acquire signing key").WithCause(err)
	}

	b := jwxjwt.NewBuilder().
		Subject(subject).
		JwtID(jti).
		IssuedAt(issuedAt).
		NotBefore(issuedAt).
		Expiration(expiresAt).
		Claim(claimSessionID, sessionID).
		Claim(claimKind, string(kind))
	if s.issuer != "" {
		b = b.Issuer(s.issuer)
	}
	if len(s.audience) > 0 {
		b = b.Audience(s.audience)
	}
	if len(extra) > 0 {
		b = b.Claim(claimExtra, extra)
	}

	tok, err := b.Build()
	if err != nil {
		return "", fault.Internal("failed to build token").WithCause(err)
	}
	signed, err := jwxjwt.Sign(tok, jwxjwt.WithKey(alg, key))
	if err != nil {
		return "", fault.Internal("failed to sign token").WithCause(err)
	}
	return string(signed), nil
}
