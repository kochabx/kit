package middleware

import (
	"slices"
	"time"

	"github.com/gin-gonic/gin"
)

// LoggerConfig 日志中间件配置
type LoggerConfig struct {
	// HeaderEnabled 是否记录请求头信息
	HeaderEnabled bool
	// HandlerEnabled 是否记录处理器名称
	HandlerEnabled bool
	// BodyEnabled 是否记录请求体
	BodyEnabled bool
	// SkipPaths 跳过记录的路径列表
	SkipPaths []string
	// Filter 自定义过滤函数
	Filter func(c *gin.Context) bool
}

// DefaultLoggerConfig 默认日志配置
func DefaultLoggerConfig() LoggerConfig {
	return LoggerConfig{
		HeaderEnabled:  false,
		BodyEnabled:    false,
		HandlerEnabled: false,
		SkipPaths:      []string{"/health", "/metrics", "/ping"},
		Filter:         nil,
	}
}

// GinLogger 创建默认的 Gin 日志中间件
func GinLogger() gin.HandlerFunc {
	return GinLoggerWithConfig(DefaultLoggerConfig())
}

// GinLoggerWithConfig 根据配置创建 Gin 日志中间件
func GinLoggerWithConfig(config LoggerConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否需要跳过记录
		if shouldSkipLogging(c, config) {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()
		duration := time.Since(start)

		event := log.Info().
			Int("status", c.Writer.Status()).
			Str("method", c.Request.Method).
			Str("uri", c.Request.RequestURI).
			Dur("duration", duration).
			Str("client_ip", c.ClientIP())

		if config.HeaderEnabled {
			event = event.Any("headers", c.Request.Header)
		}

		if config.HandlerEnabled {
			event = event.Str("handler", c.HandlerName())
		}

		if config.BodyEnabled {
			body, err := c.GetRawData()
			if err != nil {
				c.Error(err)
			} else {
				event = event.Str("body", string(body))
			}
		}

		if requestId := c.Request.Header.Get("X-Request-Id"); requestId != "" {
			event = event.Str("request_id", requestId)
		}

		// 记录错误信息
		if len(c.Errors) > 0 {
			event = event.Any("errors", c.Errors.ByType(gin.ErrorTypePrivate).String())
		}

		event.Send()
	}
}

// shouldSkipLogging 是否应该跳过日志记录
func shouldSkipLogging(c *gin.Context, config LoggerConfig) bool {
	// 检查自定义过滤器
	if config.Filter != nil {
		return config.Filter(c)
	}

	// 检查跳过路径列表
	path := c.Request.URL.Path
	return slices.Contains(config.SkipPaths, path)
}
