package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// Logger middleware tests.
// The request logger tests verify:
//   - Response status codes and bodies pass through unchanged
//   - Skip.Paths and Skip.Func bypass middleware consistently
//   - Downstream handlers can still read the request body after logging
//
// Verify consistent behavior with net/http and Gin.
// ============================================================================

func TestLogger_PassesThrough(t *testing.T) {
	mw := Logger()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})

	w := do(mw(inner), http.MethodPost, "/resource", nil)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if w.Body.String() != "created" {
		t.Errorf("body = %q, want %q", w.Body.String(), "created")
	}
}

func TestLogger_SkipPaths(t *testing.T) {
	mw := Logger(LoggerConfig{
		Skip: SkipConfig{Paths: []string{"/health"}},
	})

	w := do(mw(okHandler), http.MethodGet, "/health", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestLogger_SkipFunc(t *testing.T) {
	mw := Logger(LoggerConfig{
		Skip: SkipConfig{Func: func(r *http.Request) bool {
			return r.Header.Get("X-Internal") == "1"
		}},
	})

	w := do(mw(okHandler), http.MethodGet, "/any", func(r *http.Request) {
		r.Header.Set("X-Internal", "1")
	})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestLogger_RequestBodyPassedToNext(t *testing.T) {
	// Request body logging must restore Body for downstream readers.
	var downstreamBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		downstreamBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"msg":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	Logger(LoggerConfig{Fields: LogFields{RequestBody: true}})(inner).ServeHTTP(w, req)

	if downstreamBody != `{"msg":"hello"}` {
		t.Errorf("downstream body = %q, want original body", downstreamBody)
	}
}

func TestLogger_ResponseBodyCapture(t *testing.T) {
	// Response body logging must preserve the response delivered to the client.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response data"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	Logger(LoggerConfig{Fields: LogFields{ResponseBody: true}})(inner).ServeHTTP(w, req)

	if w.Body.String() != "response data" {
		t.Errorf("client body = %q, want %q", w.Body.String(), "response data")
	}
}

func TestLogger_DefaultStatus200(t *testing.T) {
	// The status defaults to HTTP 200 when WriteHeader is not called.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	w := do(Logger()(inner), http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestLogger_ClientIP_XRealIP(t *testing.T) {
	// Test clientIP directly.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "1.2.3.4")
	capturedIP := clientIP(req)
	if capturedIP != "1.2.3.4" {
		t.Errorf("X-Real-IP: clientIP = %q, want %q", capturedIP, "1.2.3.4")
	}
}

func TestLogger_ClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	ip := clientIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("X-Forwarded-For: clientIP = %q, want %q", ip, "10.0.0.1")
	}
}

func TestLogger_ClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:5678"
	ip := clientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("RemoteAddr: clientIP = %q, want %q", ip, "192.168.1.1")
	}
}

// ============================================================================
// Gin integration test.
// ============================================================================

func TestLogger_Gin(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("gin ok"))
	})

	r := ginEngine(http.MethodGet, "/log", Logger(), inner)

	req := httptest.NewRequest(http.MethodGet, "/log", nil)
	req.Header.Set("X-Request-Id", "req-abc")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("[Gin] status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if w.Body.String() != "gin ok" {
		t.Errorf("[Gin] body = %q, want %q", w.Body.String(), "gin ok")
	}
}
