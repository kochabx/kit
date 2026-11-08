package middleware

import (
	"context"
	"net/http"
	"slices"

	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	kithttp "github.com/kochabx/kit/transport/http"
)

var (
	ErrPermissionCheckerNil = errors.Internal("permission checker missing")
	ErrUnauthorized         = errors.Unauthorized("unauthorized")
	ErrForbidden            = errors.Forbidden("forbidden")
)

// PermissionChecker checks request permissions.
type PermissionChecker interface {
	Check(ctx context.Context, r *http.Request) error
}

// PermissionCheckerFunc adapts a function to PermissionChecker.
type PermissionCheckerFunc func(ctx context.Context, r *http.Request) error

func (f PermissionCheckerFunc) Check(ctx context.Context, r *http.Request) error {
	return f(ctx, r)
}

// PermissionConfig configures permission middleware.
type PermissionConfig struct {
	Skip           SkipConfig                                      // Skip rules.
	Checker        PermissionChecker                               // Required permission checker.
	SuccessHandler func(http.ResponseWriter, *http.Request)        // Success callback.
	ErrorHandler   func(http.ResponseWriter, *http.Request, error) // Error handler.
	Logger         *log.Logger                                     // Custom logger.
}

// Permission creates permission-checking middleware.
func Permission(cfgs ...PermissionConfig) func(http.Handler) http.Handler {
	var cfg PermissionConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.Global()
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			if e, ok := errors.From(err); ok {
				kithttp.Fail(w, e.Code(), e.Code(), err)
				return
			}
			kithttp.Fail(w, http.StatusInternalServerError, http.StatusInternalServerError, err)
		}
	}

	matcher := NewPathMatcher(cfg.Skip.Paths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.Skip.Func) {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.Checker == nil {
				cfg.ErrorHandler(w, r, ErrPermissionCheckerNil)
				return
			}

			if err := cfg.Checker.Check(r.Context(), r); err != nil {
				cfg.Logger.Error().Err(err).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("permission: check failed")
				cfg.ErrorHandler(w, r, err)
				return
			}

			if cfg.SuccessHandler != nil {
				cfg.SuccessHandler(w, r)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RoleBasedConfig configures role-based permission checks.
type RoleBasedConfig struct {
	AllowedRoles []string                  // Allowed roles.
	ClaimsKey    string                    // Context key holding claims.
	RolesGetter  func(claims any) []string // Callback that extracts roles from claims.
}

// RoleBasedChecker creates a role-based permission checker.
func RoleBasedChecker(cfg RoleBasedConfig) PermissionChecker {
	if cfg.ClaimsKey == "" {
		cfg.ClaimsKey = "claims"
	}

	return PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		claims := ctx.Value(cfg.ClaimsKey)
		if claims == nil {
			return ErrUnauthorized
		}

		var roles []string
		if cfg.RolesGetter != nil {
			roles = cfg.RolesGetter(claims)
		} else if rv, ok := claims.(interface{ GetRoles() []string }); ok {
			roles = rv.GetRoles()
		}

		if !hasIntersection(cfg.AllowedRoles, roles) {
			return ErrForbidden
		}

		return nil
	})
}

// OwnerBasedConfig configures ownership checks.
type OwnerBasedConfig struct {
	SkipRoles      []string                                                     // Roles that bypass ownership checks, such as administrators.
	ClaimsKey      string                                                       // Context key holding claims.
	RolesGetter    func(claims any) []string                                    // Callback that extracts roles from claims.
	OperatorGetter func(claims any) string                                      // Callback that extracts the acting subject ID from claims.
	OwnerGetter    func(ctx context.Context, r *http.Request) ([]string, error) // Callback that returns resource owner IDs.
}

// OwnerBasedChecker creates an ownership-based permission checker.
func OwnerBasedChecker(cfg OwnerBasedConfig) PermissionChecker {
	if cfg.ClaimsKey == "" {
		cfg.ClaimsKey = "claims"
	}

	return PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		claims := ctx.Value(cfg.ClaimsKey)
		if claims == nil {
			return ErrUnauthorized
		}

		var roles []string
		if cfg.RolesGetter != nil {
			roles = cfg.RolesGetter(claims)
		} else if rv, ok := claims.(interface{ GetRoles() []string }); ok {
			roles = rv.GetRoles()
		}

		if hasIntersection(cfg.SkipRoles, roles) {
			return nil
		}

		var operator string
		if cfg.OperatorGetter != nil {
			operator = cfg.OperatorGetter(claims)
		} else if s, ok := claims.(interface{ GetSubject() string }); ok {
			operator = s.GetSubject()
		}

		if operator == "" {
			return ErrUnauthorized
		}

		if cfg.OwnerGetter == nil {
			return ErrForbidden
		}

		owners, err := cfg.OwnerGetter(ctx, r)
		if err != nil {
			return err
		}

		if !slices.Contains(owners, operator) {
			return ErrForbidden
		}

		return nil
	})
}

// hasIntersection reports whether two slices share an element.
func hasIntersection[T comparable](a, b []T) bool {
	for _, item := range b {
		if slices.Contains(a, item) {
			return true
		}
	}
	return false
}
