package middleware

import (
	"context"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/kochabx/kit/errors"
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/transport/http"
)

var (
	ErrUnauthorized = errors.Unauthorized("unauthorized")
	ErrForbidden    = errors.Forbidden("forbidden")
)

// PermissionChecker 权限检查器接口
type PermissionChecker interface {
	Check(ctx context.Context, c *gin.Context) error
}

// PermissionCheckerFunc 权限检查器函数适配器
type PermissionCheckerFunc func(ctx context.Context, c *gin.Context) error

func (f PermissionCheckerFunc) Check(ctx context.Context, c *gin.Context) error {
	return f(ctx, c)
}

// PermissionConfig 权限中间件配置
type PermissionConfig struct {
	Checker      PermissionChecker         // 权限检查器（必需）
	SkipPaths    []string                  // 跳过检查的路径前缀
	SkipFunc     func(*gin.Context) bool   // 动态跳过判断函数
	ErrorHandler func(*gin.Context, error) // 错误处理函数
	Logger       *log.Logger               // 自定义日志记录器
}

// Permission 创建权限检查中间件
func Permission(cfgs ...PermissionConfig) gin.HandlerFunc {
	var cfg PermissionConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	// 设置默认日志记录器
	if cfg.Logger == nil {
		cfg.Logger = log.G
	}

	if cfg.Checker == nil {
		panic("middleware: PermissionChecker is required")
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(c *gin.Context, err error) {
			http.GinJSONE(c, http.StatusForbidden, ErrForbidden)
			c.Abort()
		}
	}

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

		if err := cfg.Checker.Check(c.Request.Context(), c); err != nil {
			cfg.Logger.Error().Err(err).
				Str("path", c.Request.URL.Path).
				Str("method", c.Request.Method).
				Msg("permission: check failed")
			cfg.ErrorHandler(c, err)
			return
		}

		c.Next()
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

	return PermissionCheckerFunc(func(ctx context.Context, c *gin.Context) error {
		claims := ctx.Value(cfg.ClaimsKey)
		if claims == nil {
			return ErrUnauthorized
		}

		var roles []string
		if cfg.RolesGetter != nil {
			roles = cfg.RolesGetter(claims)
		} else {
			// 尝试从 claims 中获取 roles
			if r, ok := claims.(interface{ GetRoles() []string }); ok {
				roles = r.GetRoles()
			}
		}

		if !hasIntersection(cfg.AllowedRoles, roles) {
			return ErrForbidden
		}

		return nil
	})
}

// OwnerBasedConfig 基于所有权的权限检查器配置
type OwnerBasedConfig struct {
	SkipRoles      []string                                                    // 跳过检查的角色（如管理员）
	ClaimsKey      string                                                      // 从 context 获取 claims 的 key
	RolesGetter    func(claims any) []string                                   // 从 claims 获取角色
	OperatorGetter func(claims any) string                                     // 从 claims 获取操作者 ID
	OwnerGetter    func(ctx context.Context, c *gin.Context) ([]string, error) // 获取资源所有者列表
}

// OwnerBasedChecker 创建基于所有权的权限检查器
func OwnerBasedChecker(cfg OwnerBasedConfig) PermissionChecker {
	if cfg.ClaimsKey == "" {
		cfg.ClaimsKey = "claims"
	}

	return PermissionCheckerFunc(func(ctx context.Context, c *gin.Context) error {
		claims := ctx.Value(cfg.ClaimsKey)
		if claims == nil {
			return ErrUnauthorized
		}

		// 获取角色并检查是否跳过
		var roles []string
		if cfg.RolesGetter != nil {
			roles = cfg.RolesGetter(claims)
		} else if r, ok := claims.(interface{ GetRoles() []string }); ok {
			roles = r.GetRoles()
		}

		if hasIntersection(cfg.SkipRoles, roles) {
			return nil
		}

		// 获取操作者
		var operator string
		if cfg.OperatorGetter != nil {
			operator = cfg.OperatorGetter(claims)
		} else if s, ok := claims.(interface{ GetSubject() string }); ok {
			operator = s.GetSubject()
		}

		if operator == "" {
			return ErrUnauthorized
		}

		// 获取所有者列表
		if cfg.OwnerGetter == nil {
			return ErrForbidden
		}

		owners, err := cfg.OwnerGetter(ctx, c)
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
