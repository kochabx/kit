package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// CorsConfig CORS 中间件配置
type CorsConfig struct {
	AllowOrigins     []string                // 允许的源，支持 "*" 通配符
	AllowMethods     []string                // 允许的 HTTP 方法
	AllowHeaders     []string                // 允许的请求头
	AllowCredentials bool                    // 是否允许携带凭证
	ExposeHeaders    []string                // 暴露给客户端的响应头
	MaxAge           int                     // 预检请求缓存时间（秒）
	SkipPaths        []string                // 跳过处理的路径前缀
	SkipFunc         func(*gin.Context) bool // 动态跳过判断函数
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

// Cors 创建 CORS 中间件
func Cors(cfgs ...CorsConfig) gin.HandlerFunc {
	cfg := DefaultCorsConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	// 预处理：判断是否允许所有源
	allowAllOrigins := len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*"

	// 预构建响应头值
	methodsHeader := strings.Join(cfg.AllowMethods, ", ")
	headersHeader := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeader := strings.Join(cfg.ExposeHeaders, ", ")
	maxAgeHeader := strconv.Itoa(cfg.MaxAge)
	credentialsHeader := strconv.FormatBool(cfg.AllowCredentials)

	// 预编译路径匹配器
	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(c *gin.Context) {
		// 检查是否跳过
		if shouldSkip(c, matcher, cfg.SkipFunc) {
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		// 检查是否允许该源
		allowed := allowAllOrigins || isOriginAllowed(origin, cfg.AllowOrigins)
		if !allowed {
			c.Next()
			return
		}

		// 设置响应头
		header := c.Writer.Header()
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

		// 处理预检请求
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
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
