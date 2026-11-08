package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// okHandler is a minimal handler returning HTTP 200.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
})

// withContextValue seeds request context for permission checks and similar tests.
func withContextValue(key, value any) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), key, value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ginEngine adapts standard middleware to Gin routes for framework integration tests.
func ginEngine(method, path string, mw func(http.Handler) http.Handler, inner http.Handler) *gin.Engine {
	r := gin.New()
	wrapped := gin.WrapH(mw(inner))
	switch method {
	case http.MethodGet:
		r.GET(path, wrapped)
	case http.MethodPost:
		r.POST(path, wrapped)
	case http.MethodOptions:
		r.OPTIONS(path, wrapped)
	default:
		r.Any(path, wrapped)
	}
	return r
}

// do is a convenience wrapper around httptest.
func do(handler http.Handler, method, target string, setupReq func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if setupReq != nil {
		setupReq(req)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}
