package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CorsConfig CORS 中间件配置
type CorsConfig struct {
	AllowOrigins     []string                 // 允许的源，支持 "*" 通配符
	AllowMethods     []string                 // 允许的 HTTP 方法
	AllowHeaders     []string                 // 允许的请求头
	AllowCredentials bool                     // 是否允许携带凭证
	ExposeHeaders    []string                 // 暴露给客户端的响应头
	MaxAge           int                      // 预检请求缓存时间（秒）
	SkipPaths        []string                 // 跳过处理的路径前缀
	SkipFunc         func(*http.Request) bool // 动态跳过判断函数
}

// DefaultCorsConfig 返回默认 CORS 配置
func DefaultCorsConfig() CorsConfig {
	return CorsConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization", "X-Request-Id"},
		AllowCredentials: false, // 注意：当 AllowOrigins 为 "*" 时，AllowCredentials 必须为 false
		ExposeHeaders:    []string{},
		MaxAge:           43200,
	}
}

// Cors 创建框架无关的 CORS 中间件
func Cors(cfgs ...CorsConfig) func(http.Handler) http.Handler {
	cfg := DefaultCorsConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	allowAllOrigins := len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*"
	methodsHeader := strings.Join(cfg.AllowMethods, ", ")
	headersHeader := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeader := strings.Join(cfg.ExposeHeaders, ", ")
	maxAgeHeader := strconv.Itoa(cfg.MaxAge)
	credentialsHeader := strconv.FormatBool(cfg.AllowCredentials)

	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.SkipFunc) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := allowAllOrigins || isOriginAllowed(origin, cfg.AllowOrigins)
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			if allowAllOrigins && !cfg.AllowCredentials {
				header.Set("Access-Control-Allow-Origin", "*")
			} else {
				header.Set("Access-Control-Allow-Origin", origin)
				header.Set("Vary", "Origin")
			}

			header.Set("Access-Control-Allow-Methods", methodsHeader)
			header.Set("Access-Control-Allow-Headers", headersHeader)
			header.Set("Access-Control-Allow-Credentials", credentialsHeader)

			if exposeHeader != "" {
				header.Set("Access-Control-Expose-Headers", exposeHeader)
			}
			header.Set("Access-Control-Max-Age", maxAgeHeader)

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed 检查源是否在允许列表中
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == origin {
			return true
		}
		// 支持通配符匹配，如 "*.example.com"
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // ".example.com"
			if strings.HasSuffix(origin, suffix) {
				return true
			}
		}
	}
	return false
}
