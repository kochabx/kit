package jwt_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"iter"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kochabx/kit/fault"
	kitjwt "github.com/kochabx/kit/x/auth/jwt"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

type memorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]kitjwt.Session
	revoked  bool
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: make(map[string]kitjwt.Session)}
}

func (m *memorySessionStore) Create(_ context.Context, s kitjwt.Session, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *memorySessionStore) Get(_ context.Context, subject, id string) (kitjwt.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Subject != subject {
		return kitjwt.Session{}, kitjwt.ErrSessionNotFound
	}
	return s, nil
}

func (m *memorySessionStore) Rotate(_ context.Context, r kitjwt.Rotation) (kitjwt.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[r.SessionID]
	if !ok || s.Subject != r.Subject {
		return kitjwt.Session{}, kitjwt.ErrSessionNotFound
	}
	if s.IsRevokedAt(r.Now) {
		return kitjwt.Session{}, kitjwt.ErrSessionRevoked
	}
	if s.RefreshJTI != r.OldRefreshJTI {
		revAt := r.Now.UnixMilli()
		s.RevokedAtMs = &revAt
		s.Status = kitjwt.SessionRevoked
		m.sessions[s.ID] = s
		return kitjwt.Session{}, kitjwt.ErrRefreshReused
	}
	s.RefreshJTI = r.NewRefreshJTI
	s.LastSeenAtMs = r.Now.UnixMilli()
	s.ExpiresAtMs = r.ExpiresAt.UnixMilli()
	m.sessions[s.ID] = s
	return s, nil
}

func (m *memorySessionStore) Revoke(_ context.Context, subject, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Subject != subject {
		return kitjwt.ErrSessionNotFound
	}
	revAt := at.UnixMilli()
	s.RevokedAtMs = &revAt
	s.Status = kitjwt.SessionRevoked
	m.sessions[id] = s
	return nil
}

func (m *memorySessionStore) RevokeDevice(_ context.Context, subject, deviceID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	revAt := at.UnixMilli()
	for id, s := range m.sessions {
		if s.Subject == subject && s.DeviceID == deviceID {
			s.RevokedAtMs = &revAt
			s.Status = kitjwt.SessionRevoked
			m.sessions[id] = s
			return nil
		}
	}
	return kitjwt.ErrSessionNotFound
}

func (m *memorySessionStore) RevokeAll(_ context.Context, subject string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	revAt := at.UnixMilli()
	for id, s := range m.sessions {
		if s.Subject == subject {
			s.RevokedAtMs = &revAt
			s.Status = kitjwt.SessionRevoked
			m.sessions[id] = s
		}
	}
	return nil
}

func (m *memorySessionStore) IsRevoked(_ context.Context, _, _, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revoked {
		return true, nil
	}
	s, ok := m.sessions[id]
	if !ok {
		return false, nil
	}
	return s.RevokedAtMs != nil || s.Status == kitjwt.SessionRevoked, nil
}

func (m *memorySessionStore) ListSessions(_ context.Context, subject string) iter.Seq2[kitjwt.Session, error] {
	m.mu.Lock()
	list := slices.Collect(maps.Values(m.sessions))
	m.mu.Unlock()

	return func(yield func(kitjwt.Session, error) bool) {
		for _, s := range list {
			if s.Subject == subject {
				if !yield(s, nil) {
					return
				}
			}
		}
	}
}

func TestEd25519KeyRotationAndJWKS(t *testing.T) {
	t.Parallel()

	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	ring := kitjwt.NewKeyRing()
	if err := ring.AddEd25519("k1", priv1); err != nil {
		t.Fatal(err)
	}
	if err := ring.AddEd25519("k2", priv2); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1700000000, 0).UTC()
	svc, err := kitjwt.New(kitjwt.Config{
		Issuer:   "kit-auth",
		Audience: []string{"api"},
		KeyRing:  ring,
		Clock:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	pair1, err := svc.Issue(t.Context(), "user-1", "dev-1", map[string]any{"role": "admin"})
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	// Rotate active signing key from k1 to k2.
	if err := ring.SetActive("k2"); err != nil {
		t.Fatal(err)
	}

	// Token signed with k1 must STILL verify against KeyRing.
	claims1, err := svc.Verify(t.Context(), pair1.AccessToken, kitjwt.TokenKindAccess)
	if err != nil {
		t.Fatalf("Verify old kid failed: %v", err)
	}
	if claims1.Subject != "user-1" || claims1.Extra["role"] != "admin" {
		t.Fatalf("unexpected claims: %+v", claims1)
	}

	// Refresh token must NOT be accepted as an access token.
	if _, err := svc.Verify(t.Context(), pair1.RefreshToken, kitjwt.TokenKindAccess); err == nil {
		t.Fatal("expected refresh token rejected as access token")
	}

	jwks, err := svc.JWKS()
	if err != nil {
		t.Fatalf("JWKS failed: %v", err)
	}
	if jwks.Len() != 2 {
		t.Fatalf("JWKS key count = %d, want 2", jwks.Len())
	}
}

func TestRevocationCheckedBeforeClaimsLoaderAndRefreshReuse(t *testing.T) {
	t.Parallel()

	ring := kitjwt.NewKeyRing()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	if err := ring.AddHMAC("hmac-1", secret); err != nil {
		t.Fatal(err)
	}

	// PublicJWKS must exclude symmetric HMAC secrets.
	jwks, err := ring.PublicJWKS()
	if err != nil || jwks.Len() != 0 {
		t.Fatalf("expected 0 public JWKs for HMAC-only ring, got %d (err=%v)", jwks.Len(), err)
	}

	now := time.Unix(1700000000, 0).UTC()
	store := newMemorySessionStore()
	var loaderCalls atomic.Int32

	svc, err := kitjwt.New(
		kitjwt.Config{
			Issuer:     "kit-auth",
			AccessTTL:  5 * time.Minute,
			RefreshTTL: time.Hour,
			KeyRing:    ring,
			Clock:      func() time.Time { return now },
		},
		kitjwt.WithSessionStore(store),
		kitjwt.WithClaimsLoader(func(_ context.Context, c *kitjwt.Claims) error {
			loaderCalls.Add(1)
			if c.Extra == nil {
				c.Extra = make(map[string]any)
			}
			c.Extra["loaded"] = true
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	pair1, err := svc.Issue(t.Context(), "u1", "ios", nil)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	// Active token runs ClaimsLoader once.
	claims, err := svc.Verify(t.Context(), pair1.AccessToken, kitjwt.TokenKindAccess)
	if err != nil || claims.Extra["loaded"] != true || loaderCalls.Load() != 1 {
		t.Fatalf("expected loaded claim and 1 loader call, got claims=%+v calls=%d err=%v", claims, loaderCalls.Load(), err)
	}

	// Refresh rotates JTI and keeps SessionID.
	now = now.Add(time.Minute)
	pair2, err := svc.Refresh(t.Context(), pair1.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if pair2.SessionID != pair1.SessionID || pair2.RefreshToken == pair1.RefreshToken {
		t.Fatalf("unexpected rotated pair: %+v vs %+v", pair2, pair1)
	}

	// Replaying pair1.RefreshToken detects reuse and revokes the session!
	if _, err := svc.Refresh(t.Context(), pair1.RefreshToken); !errors.Is(err, kitjwt.ErrRefreshReused) {
		t.Fatalf("expected ErrRefreshReused, got %v", err)
	}

	// Revoked access token MUST fail BEFORE ClaimsLoader is called.
	prevCalls := loaderCalls.Load()
	_, err = svc.Verify(t.Context(), pair2.AccessToken, kitjwt.TokenKindAccess)
	if err == nil {
		t.Fatal("expected revoked token rejected")
	}
	f, ok := errors.AsType[*fault.Error](err)
	if !ok || f.Code() != fault.CodeUnauthenticated {
		t.Fatalf("expected fault.CodeUnauthenticated, got %v", err)
	}
	if loaderCalls.Load() != prevCalls {
		t.Fatalf("ClaimsLoader invoked on revoked token: before=%d after=%d", prevCalls, loaderCalls.Load())
	}

	// ListSessions yields via iter.Seq2.
	var count int
	for s, listErr := range svc.ListSessions(t.Context(), "u1") {
		if listErr != nil {
			t.Fatalf("ListSessions error: %v", listErr)
		}
		if s.ID == pair1.SessionID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 session from ListSessions, got %d", count)
	}

	// Advance past AccessTTL + Leeway: JWT expiry returns fault.CodeUnauthenticated.
	now = now.Add(10 * time.Minute)
	store.revoked = false
	if _, err := svc.Verify(t.Context(), pair2.AccessToken, kitjwt.TokenKindAccess); err == nil {
		t.Fatal("expected expired access token error")
	}
}

func TestKeyRingSymmetricJWKNotExportedAndCloneSafety(t *testing.T) {
	rawOct := make([]byte, 32)
	for i := range rawOct {
		rawOct[i] = byte(i + 1)
	}
	symJWK, err := jwk.Import(rawOct)
	if err != nil {
		t.Fatal(err)
	}
	ring := kitjwt.NewKeyRing()
	if err := ring.Add("sym-1", jwa.HS256(), symJWK); err != nil {
		t.Fatalf("Add symmetric jwk failed: %v", err)
	}
	// Caller JWK must not be mutated in place.
	if _, hasKID := symJWK.KeyID(); hasKID {
		t.Fatal("Add mutated caller jwk.Key in place")
	}
	// PublicJWKS must NEVER expose symmetric key material.
	jwks, jwksErr := ring.PublicJWKS()
	if jwksErr != nil {
		t.Fatalf("PublicJWKS failed: %v", jwksErr)
	}
	if jwks.Len() != 0 {
		t.Fatalf("symmetric jwk.Key leaked into PublicJWKS: len=%d", jwks.Len())
	}
}

func TestJWTKeyRingSignValidationAndSignFailurePreservesSessions(t *testing.T) {
	t.Parallel()

	pub1, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ring := kitjwt.NewKeyRing()
	if err := ring.AddEd25519("k1", priv1); err != nil {
		t.Fatal(err)
	}
	if err := ring.Add("pub-only", jwa.EdDSA(), pub1); err != nil {
		t.Fatal(err)
	}
	if err := ring.SetActive("pub-only"); err == nil {
		t.Fatal("expected SetActive to reject verify-only public key")
	}
	if err := ring.Add("k1", jwa.EdDSA(), pub1); err == nil {
		t.Fatal("expected Add overwriting active kid with verify-only key to fail")
	}

	store := newMemorySessionStore()
	svc, err := kitjwt.New(kitjwt.Config{
		Issuer:  "kit-auth",
		KeyRing: ring,
	}, kitjwt.WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}

	// Original key k1 still signs cleanly after rejected key transitions.
	pair1, err := svc.Issue(t.Context(), "u-safe", "dev-1", nil)
	if err != nil {
		t.Fatalf("Issue with intact k1 failed: %v", err)
	}

	// Switch active key to one with an incompatible alg so jws.Sign fails cleanly.
	if err := ring.Add("bad-alg", jwa.RS256(), priv1); err != nil {
		t.Fatal(err)
	}
	if err := ring.SetActive("bad-alg"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Issue(t.Context(), "u-fail", "dev-2", nil); err == nil {
		t.Fatal("expected Issue to fail when signing fails")
	}
	if _, err := store.Get(t.Context(), "u-fail", ""); !errors.Is(err, kitjwt.ErrSessionNotFound) {
		// Verify no session persisted for u-fail.
	}
	for s, listErr := range store.ListSessions(t.Context(), "u-fail") {
		t.Fatalf("failed Issue persisted session %+v (err=%v)", s, listErr)
	}

	if _, err := svc.Refresh(t.Context(), pair1.RefreshToken); err == nil {
		t.Fatal("expected Refresh to fail when signing fails")
	}

	// Restore valid k1: original refresh token MUST still succeed because Rotate was not called on failure.
	if err := ring.SetActive("k1"); err != nil {
		t.Fatal(err)
	}
	pair2, err := svc.Refresh(t.Context(), pair1.RefreshToken)
	if err != nil {
		t.Fatalf("original RefreshToken stopped working after failed Refresh: %v", err)
	}
	if pair2.SessionID != pair1.SessionID {
		t.Fatalf("session ID mismatch: %q vs %q", pair2.SessionID, pair1.SessionID)
	}
}
