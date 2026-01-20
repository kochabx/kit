package middleware

import (
	"bytes"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/log"
)

// LoggerConfig 日志中间件配置
type LoggerConfig struct {
	RequestBody  bool                    // 是否记录请求体
	ResponseBody bool                    // 是否记录响应体
	Header       bool                    // 是否记录请求头
	HandlerName  bool                    // 是否记录处理器名称
	SkipPaths    []string                // 跳过记录的路径
	SkipFunc     func(*gin.Context) bool // 动态跳过判断函数
	Logger       *log.Logger             // 自定义日志记录器
}

// responseWriter 包装 gin.ResponseWriter 以捕获响应体
type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// Logger 创建日志中间件
func Logger(cfgs ...LoggerConfig) gin.HandlerFunc {
	cfg := LoggerConfig{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	// 设置默认日志记录器
	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	// 预编译路径匹配器
	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(c *gin.Context) {
		// 检查是否跳过
		if shouldSkip(c, matcher, cfg.SkipFunc) {
			c.Next()
			return
		}

		start := time.Now()

		// 读取请求体
		var requestBody []byte
		if cfg.RequestBody {
			body, err := c.GetRawData()
			if err == nil {
				requestBody = body
				c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
			}
		}

		// 包装 ResponseWriter
		var rw *responseWriter
		if cfg.ResponseBody {
			rw = &responseWriter{
				ResponseWriter: c.Writer,
				body:           bytes.NewBuffer(nil),
			}
			c.Writer = rw
		}

		// 处理请求
		c.Next()

		// 记录日志
		event := cfg.Logger.Info().
			Int("status", c.Writer.Status()).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Dur("duration", time.Since(start)).
			Str("client_ip", c.ClientIP())

		if query := c.Request.URL.RawQuery; query != "" {
			event = event.Str("query", query)
		}

		if requestID := c.Request.Header.Get("X-Request-Id"); requestID != "" {
			event = event.Str("request_id", requestID)
		}

		if cfg.HandlerName {
			event = event.Str("handler", c.HandlerName())
		}

		if cfg.Header {
			event = event.Any("headers", c.Request.Header)
		}

		if cfg.RequestBody && len(requestBody) > 0 {
			event = event.Bytes("request_body", requestBody)
		}

		if cfg.ResponseBody && rw != nil {
			event = event.Bytes("response_body", rw.body.Bytes())
		}

		if len(c.Errors) > 0 {
			event = event.Str("errors", c.Errors.ByType(gin.ErrorTypePrivate).String())
		}

		event.Send()
	}
}
