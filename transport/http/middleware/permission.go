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
	ErrUnauthorized = errors.Unauthorized("unauthorized")
	ErrForbidden    = errors.Forbidden("forbidden")
)

// PermissionChecker 权限检查器接口
type PermissionChecker interface {
	Check(ctx context.Context, r *http.Request) error
}

// PermissionCheckerFunc 权限检查器函数适配器
type PermissionCheckerFunc func(ctx context.Context, r *http.Request) error

func (f PermissionCheckerFunc) Check(ctx context.Context, r *http.Request) error {
	return f(ctx, r)
}

// PermissionConfig 权限中间件配置
type PermissionConfig struct {
	Checker      PermissionChecker                               // 权限检查器（必需）
	SkipPaths    []string                                        // 跳过检查的路径前缀
	SkipFunc     func(*http.Request) bool                        // 动态跳过判断函数
	ErrorHandler func(http.ResponseWriter, *http.Request, error) // 错误处理函数
	Logger       *log.Logger                                     // 自定义日志记录器
}

// Permission 创建框架无关的权限检查中间件
func Permission(cfgs ...PermissionConfig) func(http.Handler) http.Handler {
	var cfg PermissionConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	if cfg.Checker == nil {
		panic("middleware: PermissionChecker is required")
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			e := errors.FromError(err)
			kithttp.Fail(w, e.Code, err)
		}
	}

	matcher := NewPathMatcher(cfg.SkipPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r, matcher, cfg.SkipFunc) {
				next.ServeHTTP(w, r)
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

			next.ServeHTTP(w, r)
		})
	}
}

// RoleBasedConfig 基于角色的权限检查器配置
type RoleBasedConfig struct {
	AllowedRoles []string                  // 允许的角色列表
	ClaimsKey    string                    // 从 context 获取 claims 的 key
	RolesGetter  func(claims any) []string // 从 claims 获取角色的函数
}

// RoleBasedChecker 创建基于角色的权限检查器
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

// OwnerBasedConfig 基于所有权的权限检查器配置
type OwnerBasedConfig struct {
	SkipRoles      []string                                                     // 跳过检查的角色（如管理员）
	ClaimsKey      string                                                       // 从 context 获取 claims 的 key
	RolesGetter    func(claims any) []string                                    // 从 claims 获取角色
	OperatorGetter func(claims any) string                                      // 从 claims 获取操作者 ID
	OwnerGetter    func(ctx context.Context, r *http.Request) ([]string, error) // 获取资源所有者列表
}

// OwnerBasedChecker 创建基于所有权的权限检查器
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

// hasIntersection 检查两个切片是否有交集
func hasIntersection[T comparable](a, b []T) bool {
	for _, item := range b {
		if slices.Contains(a, item) {
			return true
		}
	}
	return false
}
