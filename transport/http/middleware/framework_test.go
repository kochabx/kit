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

// okHandler 返回 200 OK 的简单处理器
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
})

// withContextValue 在请求上下文中预设键值对，用于 permission 等需要上下文数据的测试
func withContextValue(key, value any) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), key, value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ginEngine 用 gin.WrapH 将 stdlib 中间件适配到 Gin 路由，展示多框架兼容性
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

// do 是 httptest 的快捷方式
func do(handler http.Handler, method, target string, setupReq func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if setupReq != nil {
		setupReq(req)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}
