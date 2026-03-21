package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// ============================================================================
// Test Claims
// ============================================================================

type TestClaims struct {
	jwt.RegisteredClaims
	UserID int64    `json:"uid"`
	Roles  []string `json:"roles"`
}

// ============================================================================
// Mock Authenticator
// ============================================================================

type mockAuthenticator struct {
	claims *TestClaims
	err    error
}

func (m *mockAuthenticator) Authenticate(ctx context.Context, token string) (*TestClaims, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.claims, nil
}

// ============================================================================
// Test Setup
// ============================================================================

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// setupHandler wraps a stdib mux with the given middleware applied globally.
func setupHandler(mw func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/protected", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims[*TestClaims](r.Context())
		if !ok {
			http.Error(w, "claims not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"user_id": claims.UserID,
			"roles":   claims.Roles,
		})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	return mw(mux)
}

// ============================================================================
// Token Extractor Tests
// ============================================================================

func TestBearerExtractor(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			header:    "Bearer mytoken123",
			wantToken: "mytoken123",
			wantErr:   false,
		},
		{
			name:      "valid bearer token with lowercase",
			header:    "bearer mytoken123",
			wantToken: "mytoken123",
			wantErr:   false,
		},
		{
			name:    "missing header",
			header:  "",
			wantErr: true,
		},
		{
			name:    "invalid prefix",
			header:  "Basic mytoken123",
			wantErr: true,
		},
		{
			name:    "bearer only no token",
			header:  "Bearer ",
			wantErr: true,
		},
	}

	extractor := BearerExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			token, err := extractor(req)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if token != tt.wantToken {
					t.Errorf("token = %q, want %q", token, tt.wantToken)
				}
			}
		})
	}
}

func TestHeaderExtractor(t *testing.T) {
	extractor := HeaderExtractor("X-API-Key")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "my-api-key")

	token, err := extractor(req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if token != "my-api-key" {
		t.Errorf("token = %q, want %q", token, "my-api-key")
	}
}

func TestQueryExtractor(t *testing.T) {
	extractor := QueryExtractor("token")

	req := httptest.NewRequest("GET", "/?token=query-token", nil)

	token, err := extractor(req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if token != "query-token" {
		t.Errorf("token = %q, want %q", token, "query-token")
	}
}

func TestCookieExtractor(t *testing.T) {
	extractor := CookieExtractor("session")

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-token"})

	token, err := extractor(req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if token != "cookie-token" {
		t.Errorf("token = %q, want %q", token, "cookie-token")
	}
}

func TestChainExtractor(t *testing.T) {
	extractor := ChainExtractor(
		HeaderExtractor("X-API-Key"),
		BearerExtractor(),
		QueryExtractor("token"),
	)

	t.Run("first extractor succeeds", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-API-Key", "api-key")

		token, err := extractor(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if token != "api-key" {
			t.Errorf("token = %q, want %q", token, "api-key")
		}
	})

	t.Run("fallback to second extractor", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer bearer-token")

		token, err := extractor(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if token != "bearer-token" {
			t.Errorf("token = %q, want %q", token, "bearer-token")
		}
	})

	t.Run("fallback to third extractor", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?token=query-token", nil)

		token, err := extractor(req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if token != "query-token" {
			t.Errorf("token = %q, want %q", token, "query-token")
		}
	})

	t.Run("all extractors fail", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		_, err := extractor(req)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

// ============================================================================
// Auth Middleware Tests
// ============================================================================

func TestAuth_Success(t *testing.T) {
	claims := &TestClaims{
		UserID: 123,
		Roles:  []string{"admin"},
	}
	claims.Subject = "user123"

	auth := &mockAuthenticator{claims: claims}
	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
	})

	handler := setupHandler(mw)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_MissingToken(t *testing.T) {
	auth := &mockAuthenticator{claims: &TestClaims{}}
	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
	})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Fail writes HTTP 200 with business code 401 in JSON body
	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}
	if !containsString(w.Body.String(), `"code":401`) {
		t.Errorf("response should contain code 401, got: %s", w.Body.String())
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
	})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}
	if !containsString(w.Body.String(), `"code":401`) {
		t.Errorf("response should contain code 401, got: %s", w.Body.String())
	}
}

func TestAuth_SkipPaths(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		SkipPaths:     []string{"/health"},
	})

	handler := setupHandler(mw)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_SkipFunc(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		SkipFunc: func(r *http.Request) bool {
			return r.Header.Get("X-Skip-Auth") == "true"
		},
	})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"skipped": true})
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("X-Skip-Auth", "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_CustomContextKey(t *testing.T) {
	claims := &TestClaims{UserID: 456}
	auth := &mockAuthenticator{claims: claims}

	var gotClaims *TestClaims
	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		ContextKey:    "user",
		SuccessHandler: func(w http.ResponseWriter, r *http.Request, c *TestClaims) {
			gotClaims = c
		},
	})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetClaims[*TestClaims](r.Context(), "user")
		if !ok {
			http.Error(w, "not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"user_id": user.UserID})
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotClaims == nil || gotClaims.UserID != 456 {
		t.Errorf("SuccessHandler not called correctly")
	}
}

func TestAuth_CustomErrorHandler(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	customErrorCalled := false

	mw := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			customErrorCalled = true
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"custom_error": err.Error()})
		},
	})

	handler := setupHandler(mw)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !customErrorCalled {
		t.Error("custom error handler not called")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestGetClaims(t *testing.T) {
	claims := &TestClaims{UserID: 789}
	ctx := context.WithValue(context.Background(), "claims", claims)

	got, ok := GetClaims[*TestClaims](ctx)
	if !ok {
		t.Error("expected ok = true")
	}
	if got.UserID != 789 {
		t.Errorf("UserID = %d, want %d", got.UserID, 789)
	}
}

func TestGetClaims_NotFound(t *testing.T) {
	ctx := context.Background()

	_, ok := GetClaims[*TestClaims](ctx)
	if ok {
		t.Error("expected ok = false")
	}
}

func TestGetClaims_TypeMismatch(t *testing.T) {
	ctx := context.WithValue(context.Background(), "claims", "not a claims struct")

	_, ok := GetClaims[*TestClaims](ctx)
	if ok {
		t.Error("expected ok = false for type mismatch")
	}
}

// ============================================================================
// AuthenticatorFunc Tests
// ============================================================================

func TestAuthenticatorFunc(t *testing.T) {
	fn := AuthenticatorFunc[*TestClaims](func(ctx context.Context, token string) (*TestClaims, error) {
		return &TestClaims{UserID: 999}, nil
	})

	claims, err := fn.Authenticate(context.Background(), "token")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if claims.UserID != 999 {
		t.Errorf("UserID = %d, want %d", claims.UserID, 999)
	}
}
