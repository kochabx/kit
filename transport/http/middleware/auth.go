package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/kochabx/kit/errors"
	kithttp "github.com/kochabx/kit/transport/http"
)

const (
	contextKey          = "claims"        // Default context key.
	headerAuthorization = "Authorization" // Authorization Header
	bearerPrefix        = "Bearer "       // Bearer token prefix.
)

var (
	ErrTokenMissing     = errors.Unauthorized("token missing")
	ErrTokenInvalid     = errors.Unauthorized("token invalid")
	ErrAuthenticatorNil = errors.Unauthorized("authenticator missing")
)

// Authenticator authenticates a token.
type Authenticator[T any] interface {
	Authenticate(ctx context.Context, token string) (T, error)
}

// AuthenticatorFunc adapts a function to Authenticator.
type AuthenticatorFunc[T any] func(ctx context.Context, token string) (T, error)

func (f AuthenticatorFunc[T]) Authenticate(ctx context.Context, token string) (T, error) {
	return f(ctx, token)
}

// TokenExtractor extracts a token from an HTTP request.
type TokenExtractor func(r *http.Request) (string, error)

// BearerExtractor reads Authorization: Bearer <token>.
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

// HeaderExtractor reads a token from the specified header.
func HeaderExtractor(header string) TokenExtractor {
	return func(r *http.Request) (string, error) {
		if token := r.Header.Get(header); token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// QueryExtractor reads a token from a URL query parameter.
func QueryExtractor(param string) TokenExtractor {
	return func(r *http.Request) (string, error) {
		if token := r.URL.Query().Get(param); token != "" {
			return token, nil
		}
		return "", ErrTokenMissing
	}
}

// CookieExtractor reads a token from the specified cookie.
func CookieExtractor(name string) TokenExtractor {
	return func(r *http.Request) (string, error) {
		c, err := r.Cookie(name)
		if err == nil && c.Value != "" {
			return c.Value, nil
		}
		return "", ErrTokenMissing
	}
}

// ChainExtractor tries extractors in order until one succeeds.
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

// AuthConfig configures authentication middleware.
type AuthConfig[T any] struct {
	Skip           SkipConfig                                      // Skip rules.
	Authenticator  Authenticator[T]                                // Required authenticator.
	Extractor      TokenExtractor                                  // Token extractor; defaults to BearerExtractor.
	ContextKey     string                                          // Context key; defaults to "claims".
	SuccessHandler func(http.ResponseWriter, *http.Request, T)     // Success callback.
	ErrorHandler   func(http.ResponseWriter, *http.Request, error) // Error handler; defaults to an HTTP 401 response.
}

// Auth creates authentication middleware.
func Auth[T any](cfg AuthConfig[T]) func(http.Handler) http.Handler {
	if cfg.Extractor == nil {
		cfg.Extractor = BearerExtractor()
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = contextKey
	}
	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			kithttp.Fail(w, http.StatusUnauthorized, http.StatusUnauthorized, err)
		}
	}

	matcher := NewPathMatcher(cfg.Skip.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.Skip.Func) {
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

// GetClaims retrieves claims from context.
func GetClaims[T any](ctx context.Context, key ...string) (T, bool) {
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
