package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
// ============================================================================
// Test Setup
// ============================================================================

func init() {
	gin.SetMode(gin.TestMode)
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

func setupRouter(middleware gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(middleware)
	r.GET("/protected", func(c *gin.Context) {
		claims, ok := GetClaims[*TestClaims](c.Request.Context())
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "claims not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id": claims.UserID,
			"roles":   claims.Roles,
		})
	})
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
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
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}

			token, err := extractor(c)

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

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("X-API-Key", "my-api-key")

	token, err := extractor(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if token != "my-api-key" {
		t.Errorf("token = %q, want %q", token, "my-api-key")
	}
}

func TestQueryExtractor(t *testing.T) {
	extractor := QueryExtractor("token")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?token=query-token", nil)

	token, err := extractor(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if token != "query-token" {
		t.Errorf("token = %q, want %q", token, "query-token")
	}
}

func TestCookieExtractor(t *testing.T) {
	extractor := CookieExtractor("session")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.AddCookie(&http.Cookie{Name: "session", Value: "cookie-token"})

	token, err := extractor(c)
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
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("X-API-Key", "api-key")

		token, err := extractor(c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if token != "api-key" {
			t.Errorf("token = %q, want %q", token, "api-key")
		}
	})

	t.Run("fallback to second extractor", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer bearer-token")

		token, err := extractor(c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if token != "bearer-token" {
			t.Errorf("token = %q, want %q", token, "bearer-token")
		}
	})

	t.Run("fallback to third extractor", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/?token=query-token", nil)

		token, err := extractor(c)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if token != "query-token" {
			t.Errorf("token = %q, want %q", token, "query-token")
		}
	})

	t.Run("all extractors fail", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		_, err := extractor(c)
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
	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
	})

	r := setupRouter(middleware)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_MissingToken(t *testing.T) {
	auth := &mockAuthenticator{claims: &TestClaims{}}
	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
	})

	r := gin.New()
	r.Use(middleware)
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// GinJSONE 返回 HTTP 200，但业务码是 401
	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}
	// 验证响应体中的业务码
	if !containsString(w.Body.String(), `"code":401`) {
		t.Errorf("response should contain code 401, got: %s", w.Body.String())
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
	})

	r := gin.New()
	r.Use(middleware)
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// GinJSONE 返回 HTTP 200，但业务码是 401
	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}
	// 验证响应体中的业务码
	if !containsString(w.Body.String(), `"code":401`) {
		t.Errorf("response should contain code 401, got: %s", w.Body.String())
	}
}

func TestAuth_SkipPaths(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		SkipPaths:     []string{"/health"},
	})

	r := setupRouter(middleware)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_SkipFunc(t *testing.T) {
	auth := &mockAuthenticator{err: ErrTokenInvalid}
	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		SkipFunc: func(c *gin.Context) bool {
			return c.GetHeader("X-Skip-Auth") == "true"
		},
	})

	r := gin.New()
	r.Use(middleware)
	r.GET("/protected", func(c *gin.Context) {
		// 跳过认证时，claims 不存在
		c.JSON(http.StatusOK, gin.H{"skipped": true})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("X-Skip-Auth", "true")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_CustomContextKey(t *testing.T) {
	claims := &TestClaims{UserID: 456}
	auth := &mockAuthenticator{claims: claims}

	var gotClaims *TestClaims
	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		ContextKey:    "user",
		SuccessHandler: func(c *gin.Context, claims *TestClaims) {
			gotClaims = claims
		},
	})

	r := gin.New()
	r.Use(middleware)
	r.GET("/test", func(c *gin.Context) {
		user, ok := GetClaims[*TestClaims](c.Request.Context(), "user")
		if !ok {
			c.JSON(http.StatusInternalServerError, nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": user.UserID})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

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

	middleware := Auth(AuthConfig[*TestClaims]{
		Authenticator: auth,
		ErrorHandler: func(c *gin.Context, err error) {
			customErrorCalled = true
			c.JSON(http.StatusForbidden, gin.H{"custom_error": err.Error()})
			c.Abort()
		},
	})

	r := setupRouter(middleware)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

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
