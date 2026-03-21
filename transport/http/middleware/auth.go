package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kochabx/kit/errors"
	kithttp "github.com/kochabx/kit/transport/http"
)

const (
	contextKey          = "claims"        // 默认上下文键
	headerAuthorization = "Authorization" // Authorization Header
	bearerPrefix        = "Bearer "       // Bearer Token 前缀
)

var (
	ErrTokenMissing     = errors.Unauthorized("token missing")
	ErrTokenInvalid     = errors.Unauthorized("token invalid")
	ErrAuthenticatorNil = errors.Unauthorized("authenticator missing")
)

// Claims JWT Claims 类型约束
type Claims = jwt.Claims

// Authenticator 认证器接口
type Authenticator[T Claims] interface {
	Authenticate(ctx context.Context, token string) (T, error)
}

// AuthenticatorFunc 函数适配器
type AuthenticatorFunc[T Claims] func(ctx context.Context, token string) (T, error)

func (f AuthenticatorFunc[T]) Authenticate(ctx context.Context, token string) (T, error) {
	return f(ctx, token)
}

// TokenExtractor 从 HTTP 请求提取 Token
type TokenExtractor func(r *http.Request) (string, error)

// BearerExtractor 从 Authorization: Bearer <token> 提取
func BearerExtractor() TokenExtractor {
	return func(r *http.Request) (string, error) {
		auth := r.Header.Get(headerAuthorization)
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
	return func(r *http.Request) (string, error) {
		if token := r.Header.Get(header); token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// QueryExtractor 从 URL Query 参数提取
func QueryExtractor(param string) TokenExtractor {
	return func(r *http.Request) (string, error) {
		if token := r.URL.Query().Get(param); token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// CookieExtractor 从 Cookie 提取
func CookieExtractor(name string) TokenExtractor {
	return func(r *http.Request) (string, error) {
		c, err := r.Cookie(name)
		if err == nil && c.Value != "" {
			return c.Value, nil
		}
		return "", ErrTokenMissing
	}
}

// ChainExtractor 链式提取器，依次尝试直到成功
func ChainExtractor(extractors ...TokenExtractor) TokenExtractor {
	return func(r *http.Request) (string, error) {
		for _, extract := range extractors {
			if token, err := extract(r); err == nil {
				return token, nil
			}
		}
		return "", ErrTokenMissing
	}
}

// AuthConfig 认证中间件配置
type AuthConfig[T Claims] struct {
	Authenticator  Authenticator[T]                                // 认证器（必需）
	Extractor      TokenExtractor                                  // Token 提取器，默认 BearerExtractor
	ContextKey     string                                          // 上下文键，默认 "claims"
	SkipPaths      []string                                        // 跳过认证的路径前缀
	SkipFunc       func(*http.Request) bool                        // 动态跳过判断
	SuccessHandler func(http.ResponseWriter, *http.Request, T)     // 成功回调
	ErrorHandler   func(http.ResponseWriter, *http.Request, error) // 错误处理，默认返回 401
}

// Auth 创建框架无关的认证中间件
func Auth[T Claims](cfg AuthConfig[T]) func(http.Handler) http.Handler {
	if cfg.Extractor == nil {
		cfg.Extractor = BearerExtractor()
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = contextKey
	}
	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			kithttp.Fail(w, http.StatusUnauthorized, err)
		}
	}

	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.SkipFunc) {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.Authenticator == nil {
				cfg.ErrorHandler(w, r, ErrAuthenticatorNil)
				return
			}

			token, err := cfg.Extractor(r)
			if err != nil {
				cfg.ErrorHandler(w, r, err)
				return
			}

			claims, err := cfg.Authenticator.Authenticate(r.Context(), token)
			if err != nil {
				cfg.ErrorHandler(w, r, err)
				return
			}

			ctx := context.WithValue(r.Context(), cfg.ContextKey, claims)
			r = r.WithContext(ctx)

			if cfg.SuccessHandler != nil {
				cfg.SuccessHandler(w, r, claims)
			}
			next.ServeHTTP(w, r)
		})
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
