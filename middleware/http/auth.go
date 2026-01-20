package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/transport/http"
)

const (
	contextKey          = "claims"        // 默认上下文键
	headerAuthorization = "Authorization" // Authorization Header
	bearerPrefix        = "Bearer "       // Bearer Token 前缀
)

var (
	ErrTokenMissing     = errors.Unauthorized("token missing")
	ErrTokenInvalid     = errors.Unauthorized("token invalid")
	ErrAuthenticatorNil = errors.Unauthorized("authenticator not configured")
)

// Claims 认证信息接口
type Claims interface {
	GetSubject() string
}

// Authenticator 认证器接口
type Authenticator[T Claims] interface {
	Authenticate(ctx context.Context, token string) (T, error)
}

// AuthenticatorFunc 函数适配器
type AuthenticatorFunc[T Claims] func(ctx context.Context, token string) (T, error)

func (f AuthenticatorFunc[T]) Authenticate(ctx context.Context, token string) (T, error) {
	return f(ctx, token)
}

// TokenExtractor Token 提取器
type TokenExtractor func(c *gin.Context) (string, error)

// BearerExtractor 从 Authorization: Bearer <token> 提取
func BearerExtractor() TokenExtractor {
	return func(c *gin.Context) (string, error) {
		auth := c.GetHeader(headerAuthorization)
		if auth == "" {
			return "", ErrTokenMissing
		}
		if len(auth) <= len(bearerPrefix) || !strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
			return "", ErrTokenInvalid
		}
		return auth[len(bearerPrefix):], nil
	}
}

// HeaderExtractor 从指定 Header 提取
func HeaderExtractor(header string) TokenExtractor {
	return func(c *gin.Context) (string, error) {
		if token := c.GetHeader(header); token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// QueryExtractor 从 URL Query 参数提取
func QueryExtractor(param string) TokenExtractor {
	return func(c *gin.Context) (string, error) {
		if token := c.Query(param); token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// CookieExtractor 从 Cookie 提取
func CookieExtractor(name string) TokenExtractor {
	return func(c *gin.Context) (string, error) {
		if token, err := c.Cookie(name); err == nil && token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// ChainExtractor 链式提取器，依次尝试直到成功
func ChainExtractor(extractors ...TokenExtractor) TokenExtractor {
	return func(c *gin.Context) (string, error) {
		for _, extract := range extractors {
			if token, err := extract(c); err == nil {
				return token, nil
			}
		}
		return "", ErrTokenMissing
	}
}

// AuthConfig 认证中间件配置
type AuthConfig[T Claims] struct {
	Authenticator  Authenticator[T]          // 认证器（必需）
	Extractor      TokenExtractor            // Token 提取器
	ContextKey     string                    // 上下文键
	SkipPaths      []string                  // 跳过认证的路径前缀
	SkipFunc       func(*gin.Context) bool   // 动态跳过判断
	SuccessHandler func(*gin.Context, T)     // 成功回调
	ErrorHandler   func(*gin.Context, error) // 错误处理
}

// Auth 创建认证中间件
func Auth[T Claims](cfg AuthConfig[T]) gin.HandlerFunc {
	if cfg.Extractor == nil {
		cfg.Extractor = BearerExtractor()
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = contextKey
	}
	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(c *gin.Context, err error) {
			http.GinJSONE(c, http.StatusUnauthorized, err)
			c.Abort()
		}
	}

	// 预编译路径匹配器
	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(c *gin.Context) {
		// 跳过检查
		if shouldSkip(c, matcher, cfg.SkipFunc) {
			c.Next()
			return
		}

		if cfg.Authenticator == nil {
			cfg.ErrorHandler(c, ErrAuthenticatorNil)
			return
		}

		token, err := cfg.Extractor(c)
		if err != nil {
			cfg.ErrorHandler(c, err)
			return
		}

		claims, err := cfg.Authenticator.Authenticate(c.Request.Context(), token)
		if err != nil {
			cfg.ErrorHandler(c, err)
			return
		}

		// 存入 request context
		ctx := context.WithValue(c.Request.Context(), cfg.ContextKey, claims)
		c.Request = c.Request.WithContext(ctx)

		if cfg.SuccessHandler != nil {
			cfg.SuccessHandler(c, claims)
		}
		c.Next()
	}
}

// GetClaims 从 Context 获取 claims
func GetClaims[T Claims](ctx context.Context, key ...string) (T, bool) {
	var zero T
	k := contextKey
	if len(key) > 0 && key[0] != "" {
		k = key[0]
	}
	if v := ctx.Value(k); v != nil {
		if claims, ok := v.(T); ok {
			return claims, true
		}
	}
	return zero, false
}
