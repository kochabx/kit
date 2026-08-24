package jwt_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	kitjwt "github.com/kochabx/kit/core/auth/jwt"
	"github.com/stretchr/testify/require"
)

type testClaims struct {
	kitjwt.SessionClaims
	Role string `json:"role"`
}
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeStore struct {
	mu       sync.Mutex
	sessions map[string]kitjwt.Session
}

func newFakeStore() *fakeStore                     { return &fakeStore{sessions: map[string]kitjwt.Session{}} }
func (s *fakeStore) key(subject, id string) string { return subject + "\x00" + id }
func (s *fakeStore) Create(_ context.Context, v kitjwt.Session, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[s.key(v.Subject, v.ID)] = v
	return nil
}
func (s *fakeStore) Get(_ context.Context, subject, id string) (*kitjwt.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[s.key(subject, id)]
	if !ok {
		return nil, kitjwt.ErrSessionNotFound
	}
	return &v, nil
}
func (s *fakeStore) Rotate(_ context.Context, r kitjwt.Rotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(r.Subject, r.SessionID)
	v, ok := s.sessions[k]
	if !ok {
		return kitjwt.ErrSessionNotFound
	}
	if v.RevokedAt != nil {
		return kitjwt.ErrSessionRevoked
	}
	if v.RefreshJTI != r.OldJTI {
		now := r.Now
		v.RevokedAt = &now
		v.Status = kitjwt.SessionRevoked
		s.sessions[k] = v
		return kitjwt.ErrRefreshReused
	}
	v.RefreshJTI = r.NewJTI
	v.ExpiresAt = r.ExpiresAt
	s.sessions[k] = v
	return nil
}
func (s *fakeStore) Revoke(_ context.Context, subject, id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(subject, id)
	v, ok := s.sessions[k]
	if !ok {
		return kitjwt.ErrSessionNotFound
	}
	v.RevokedAt = &now
	v.Status = kitjwt.SessionRevoked
	s.sessions[k] = v
	return nil
}
func (s *fakeStore) RevokeAll(_ context.Context, subject string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if v.Subject == subject {
			v.RevokedAt = &now
			v.Status = kitjwt.SessionRevoked
			s.sessions[k] = v
		}
	}
	return nil
}
func (s *fakeStore) RevokeDevice(_ context.Context, subject, deviceID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for k, v := range s.sessions {
		if v.Subject == subject && v.DeviceID == deviceID {
			v.RevokedAt = &now
			v.Status = kitjwt.SessionRevoked
			s.sessions[k] = v
			found = true
		}
	}
	if !found {
		return kitjwt.ErrSessionNotFound
	}
	return nil
}
func (s *fakeStore) List(_ context.Context, subject string, query kitjwt.SessionQuery) (kitjwt.SessionPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []kitjwt.Session
	for _, v := range s.sessions {
		if v.Subject == subject && (query.Status == "" || v.Status == query.Status) {
			out = append(out, v)
		}
	}
	return kitjwt.SessionPage{Sessions: out}, nil
}

func newAuth(t *testing.T) *kitjwt.Auth[*testClaims] {
	t.Helper()
	key, err := kitjwt.NewHMACKeyProvider("key-1", gojwt.SigningMethodHS256, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	auth, err := kitjwt.New(kitjwt.AuthConfig[*testClaims]{
		ClaimsFactory: func() *testClaims { return new(testClaims) }, KeyProvider: key, Store: newFakeStore(),
		ClaimsLoader: func(_ context.Context, subject string) (*testClaims, error) {
			claims := new(testClaims)
			claims.Subject = subject
			claims.Role = "admin"
			return claims, nil
		},
		Clock:           fixedClock{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		Issuer:          "issuer",
		AccessAudience:  []string{"api"},
		RefreshAudience: []string{"token"},
		AccessTTL:       15 * time.Minute,
		RefreshTTL:      24 * time.Hour,
		Leeway:          time.Second,
		MaxSessions:     3,
	})
	require.NoError(t, err)
	return auth
}

func TestNewRejectsOverlappingTokenAudiences(t *testing.T) {
	key, err := kitjwt.NewHMACKeyProvider(
		"key-1",
		gojwt.SigningMethodHS256,
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)

	_, err = kitjwt.New(kitjwt.AuthConfig[*testClaims]{
		ClaimsFactory:   func() *testClaims { return new(testClaims) },
		ClaimsLoader:    func(context.Context, string) (*testClaims, error) { return new(testClaims), nil },
		KeyProvider:     key,
		Store:           newFakeStore(),
		Issuer:          "issuer",
		AccessAudience:  []string{"shared"},
		RefreshAudience: []string{"shared"},
		AccessTTL:       time.Minute,
		RefreshTTL:      time.Hour,
		MaxSessions:     1,
	})
	require.ErrorIs(t, err, kitjwt.ErrConfigInvalid)
}

func TestNewRejectsNilClaimsFromFactory(t *testing.T) {
	key, err := kitjwt.NewHMACKeyProvider(
		"key-1",
		gojwt.SigningMethodHS256,
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = kitjwt.New(kitjwt.AuthConfig[*testClaims]{
			ClaimsFactory:   func() *testClaims { return nil },
			ClaimsLoader:    func(context.Context, string) (*testClaims, error) { return nil, nil },
			KeyProvider:     key,
			Store:           newFakeStore(),
			Issuer:          "issuer",
			AccessAudience:  []string{"api"},
			RefreshAudience: []string{"refresh"},
			AccessTTL:       time.Minute,
			RefreshTTL:      time.Hour,
			MaxSessions:     1,
		})
	})
	require.ErrorIs(t, err, kitjwt.ErrConfigInvalid)
}

func TestIssueRejectsNilClaims(t *testing.T) {
	auth := newAuth(t)
	require.NotPanics(t, func() {
		_, err := auth.Issue(context.Background(), nil)
		require.ErrorIs(t, err, kitjwt.ErrInvalidClaims)
	})
}

func TestServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	service := newAuth(t)
	claims := new(testClaims)
	claims.Subject = "user-1"
	claims.Role = "admin"
	pair, err := service.Issue(ctx, claims, kitjwt.WithIssueDeviceID("phone"))
	require.NoError(t, err)
	identity, err := service.Authenticate(ctx, pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "user-1", identity.Subject)
	require.Equal(t, "admin", identity.Claims.Role)
	require.Equal(t, "phone", identity.DeviceID)
	_, err = service.Authenticate(ctx, strings.Repeat("x", 8193))
	require.ErrorIs(t, err, kitjwt.ErrTokenTooLarge)
	_, err = service.Authenticate(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, kitjwt.ErrInvalidAudience)
	rotated, err := service.Refresh(ctx, pair.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, pair.RefreshToken, rotated.RefreshToken)
	_, err = service.Refresh(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, kitjwt.ErrRefreshReused)
	_, err = service.Authenticate(ctx, rotated.AccessToken)
	require.ErrorIs(t, err, kitjwt.ErrSessionRevoked)
}

func TestIssuedTokensExposeOnlySessionID(t *testing.T) {
	auth := newAuth(t)
	claims := new(testClaims)
	claims.Subject = "user-1"
	pair, err := auth.Issue(context.Background(), claims)
	require.NoError(t, err)

	for _, raw := range []string{pair.AccessToken, pair.RefreshToken} {
		payload := gojwt.MapClaims{}
		_, _, err = gojwt.NewParser().ParseUnverified(raw, payload)
		require.NoError(t, err)
		require.NotEmpty(t, payload["sid"])
		require.NotContains(t, payload, "token_use")
		require.NotContains(t, payload, "family")
		require.NotContains(t, payload, "family_id")
	}
}

func TestRefreshIsSingleUseUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	service := newAuth(t)
	claims := new(testClaims)
	claims.Subject = "user-1"
	pair, err := service.Issue(ctx, claims)
	require.NoError(t, err)
	var success atomic.Int32
	var reused atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Refresh(ctx, pair.RefreshToken)
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, kitjwt.ErrRefreshReused) || errors.Is(err, kitjwt.ErrSessionRevoked) {
				reused.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	require.EqualValues(t, 31, reused.Load())
}

func TestRevokeDevice(t *testing.T) {
	ctx := context.Background()
	auth := newAuth(t)
	claims := new(testClaims)
	claims.Subject = "user-1"
	pair, err := auth.Issue(ctx, claims, kitjwt.WithIssueDeviceID("phone"))
	require.NoError(t, err)
	require.NoError(t, auth.RevokeDevice(ctx, "user-1", "phone"))
	_, err = auth.Authenticate(ctx, pair.AccessToken)
	require.ErrorIs(t, err, kitjwt.ErrSessionRevoked)
	require.ErrorIs(t, auth.RevokeDevice(ctx, "user-1", "missing"), kitjwt.ErrSessionNotFound)
}
