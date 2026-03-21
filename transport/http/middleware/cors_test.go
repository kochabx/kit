package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// CORS 中间件测试
// 展示：stdlib net/http 与 Gin 两种框架下的一致行为
// ============================================================================

func TestCors_AllowAllOrigins(t *testing.T) {
	mw := Cors(DefaultCorsConfig())
	handler := mw(okHandler)

	w := do(handler, http.MethodGet, "/test", func(r *http.Request) {
		r.Header.Set("Origin", "http://any-domain.com")
	})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCors_SpecificOrigin(t *testing.T) {
	cfg := CorsConfig{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET"},
	}
	handler := Cors(cfg)(okHandler)

	t.Run("allowed origin", func(t *testing.T) {
		w := do(handler, http.MethodGet, "/test", func(r *http.Request) {
			r.Header.Set("Origin", "https://example.com")
		})
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
			t.Errorf("Allow-Origin = %q, want %q", got, "https://example.com")
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		w := do(handler, http.MethodGet, "/test", func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.com")
		})
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin should be empty for disallowed origin, got %q", got)
		}
	})
}

func TestCors_NoOriginHeader(t *testing.T) {
	handler := Cors()(okHandler)

	// 没有 Origin 头时，不设置 CORS 响应头，直接通过
	w := do(handler, http.MethodGet, "/test", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("should not set CORS headers without Origin, got %q", got)
	}
}

func TestCors_Preflight(t *testing.T) {
	handler := Cors(DefaultCorsConfig())(okHandler)

	w := do(handler, http.MethodOptions, "/test", func(r *http.Request) {
		r.Header.Set("Origin", "http://example.com")
		r.Header.Set("Access-Control-Request-Method", "POST")
	})

	if w.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods should be set on preflight")
	}
}

func TestCors_SkipPaths(t *testing.T) {
	cfg := CorsConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET"},
		SkipPaths:    []string{"/internal"},
	}
	handler := Cors(cfg)(okHandler)

	w := do(handler, http.MethodGet, "/internal", func(r *http.Request) {
		r.Header.Set("Origin", "http://example.com")
	})

	// 跳过的路径不设置 CORS 响应头
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("skipped path should not have CORS headers, got %q", got)
	}
}

func TestCors_WildcardSubdomain(t *testing.T) {
	cfg := CorsConfig{
		AllowOrigins: []string{"*.example.com"},
		AllowMethods: []string{"GET"},
	}
	handler := Cors(cfg)(okHandler)

	w := do(handler, http.MethodGet, "/test", func(r *http.Request) {
		r.Header.Set("Origin", "https://app.example.com")
	})

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Allow-Origin = %q, want subdomain allowed", got)
	}
}

// ============================================================================
// Gin 集成测试 — 同一中间件通过 gin.WrapH 在 Gin 框架下运行
// ============================================================================

func TestCors_Gin(t *testing.T) {
	r := ginEngine(http.MethodGet, "/api/data",
		Cors(DefaultCorsConfig()),
		okHandler,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[Gin] status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("[Gin] Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCors_Gin_Preflight(t *testing.T) {
	r := ginEngine(http.MethodOptions, "/api/data",
		Cors(DefaultCorsConfig()),
		okHandler,
	)

	req := httptest.NewRequest(http.MethodOptions, "/api/data", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("[Gin] preflight status = %d, want %d", w.Code, http.StatusNoContent)
	}
}
