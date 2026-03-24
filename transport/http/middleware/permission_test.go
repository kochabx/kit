package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// Permission 中间件测试
// 展示：stdlib net/http 与 Gin 两种框架下的一致行为
// ============================================================================

// permClaims 实现 GetRoles / GetSubject 接口，供 RoleBasedChecker / OwnerBasedChecker 使用
type permClaims struct {
	subject string
	roles   []string
}

func (c *permClaims) GetRoles() []string { return c.roles }
func (c *permClaims) GetSubject() string { return c.subject }

func TestPermission_Allow(t *testing.T) {
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return nil
	})
	mw := Permission(PermissionConfig{Checker: checker})

	w := do(mw(okHandler), http.MethodGet, "/resource", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestPermission_Deny(t *testing.T) {
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return ErrForbidden
	})
	mw := Permission(PermissionConfig{Checker: checker})

	w := do(mw(okHandler), http.MethodGet, "/resource", nil)
	// Fail 写入 HTTP 200 + 业务码 403
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (Fail uses business code)", w.Code, http.StatusOK)
	}
	if !containsString(w.Body.String(), `"code":403`) {
		t.Errorf("body should contain code 403, got: %s", w.Body.String())
	}
}

func TestPermission_SkipPaths(t *testing.T) {
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return ErrForbidden // 如果被调用一定拒绝
	})
	mw := Permission(PermissionConfig{
		Checker: checker,
		Skip:    SkipConfig{Paths: []string{"/public"}},
	})

	w := do(mw(okHandler), http.MethodGet, "/public", nil)
	if w.Code != http.StatusOK {
		t.Errorf("skipped path should pass, status = %d", w.Code)
	}
}

func TestPermission_SkipFunc(t *testing.T) {
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return ErrForbidden
	})
	mw := Permission(PermissionConfig{
		Checker: checker,
		Skip: SkipConfig{Func: func(r *http.Request) bool {
			return r.Header.Get("X-Service") == "internal"
		}},
	})

	w := do(mw(okHandler), http.MethodGet, "/any", func(r *http.Request) {
		r.Header.Set("X-Service", "internal")
	})
	if w.Code != http.StatusOK {
		t.Errorf("SkipFunc should bypass, status = %d", w.Code)
	}
}

func TestPermission_CustomErrorHandler(t *testing.T) {
	customCalled := false
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return ErrForbidden
	})
	mw := Permission(PermissionConfig{
		Checker: checker,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			customCalled = true
			w.WriteHeader(http.StatusForbidden)
		},
	})

	w := do(mw(okHandler), http.MethodGet, "/resource", nil)
	if !customCalled {
		t.Error("custom error handler should be called")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// ============================================================================
// RoleBasedChecker 测试
// ============================================================================

func TestRoleBasedChecker_Allow(t *testing.T) {
	checker := RoleBasedChecker(RoleBasedConfig{AllowedRoles: []string{"admin", "editor"}})
	mw := Permission(PermissionConfig{Checker: checker})

	claims := &permClaims{roles: []string{"editor"}}
	handler := withContextValue("claims", claims)(mw(okHandler))

	w := do(handler, http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Errorf("editor should be allowed, status = %d", w.Code)
	}
}

func TestRoleBasedChecker_Deny(t *testing.T) {
	checker := RoleBasedChecker(RoleBasedConfig{AllowedRoles: []string{"admin"}})
	mw := Permission(PermissionConfig{Checker: checker})

	claims := &permClaims{roles: []string{"viewer"}}
	handler := withContextValue("claims", claims)(mw(okHandler))

	w := do(handler, http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (Fail uses business code)", w.Code, http.StatusOK)
	}
	if !containsString(w.Body.String(), `"code":403`) {
		t.Errorf("viewer should be forbidden, got: %s", w.Body.String())
	}
}

func TestRoleBasedChecker_NoClaims(t *testing.T) {
	checker := RoleBasedChecker(RoleBasedConfig{AllowedRoles: []string{"admin"}})
	mw := Permission(PermissionConfig{Checker: checker})

	// 无 claims → ErrUnauthorized
	w := do(mw(okHandler), http.MethodGet, "/", nil)
	if !containsString(w.Body.String(), `"code":401`) {
		t.Errorf("no claims should return 401, got: %s", w.Body.String())
	}
}

func TestRoleBasedChecker_CustomKey(t *testing.T) {
	checker := RoleBasedChecker(RoleBasedConfig{
		AllowedRoles: []string{"admin"},
		ClaimsKey:    "user",
	})
	mw := Permission(PermissionConfig{Checker: checker})

	claims := &permClaims{roles: []string{"admin"}}
	handler := withContextValue("user", claims)(mw(okHandler))

	w := do(handler, http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Errorf("admin with custom key should be allowed, status = %d", w.Code)
	}
}

func TestRoleBasedChecker_CustomRolesGetter(t *testing.T) {
	type customClaims struct{ role string }
	checker := RoleBasedChecker(RoleBasedConfig{
		AllowedRoles: []string{"superuser"},
		RolesGetter: func(claims any) []string {
			if c, ok := claims.(*customClaims); ok {
				return []string{c.role}
			}
			return nil
		},
	})
	mw := Permission(PermissionConfig{Checker: checker})

	handler := withContextValue("claims", &customClaims{role: "superuser"})(mw(okHandler))
	w := do(handler, http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Errorf("superuser should be allowed, status = %d", w.Code)
	}
}

// ============================================================================
// OwnerBasedChecker 测试
// ============================================================================

func TestOwnerBasedChecker_Allow(t *testing.T) {
	checker := OwnerBasedChecker(OwnerBasedConfig{
		OwnerGetter: func(ctx context.Context, r *http.Request) ([]string, error) {
			return []string{"user-1", "user-2"}, nil
		},
	})
	mw := Permission(PermissionConfig{Checker: checker})

	claims := &permClaims{subject: "user-1"}
	handler := withContextValue("claims", claims)(mw(okHandler))

	w := do(handler, http.MethodGet, "/resource/1", nil)
	if w.Code != http.StatusOK {
		t.Errorf("owner should be allowed, status = %d", w.Code)
	}
}

func TestOwnerBasedChecker_Deny(t *testing.T) {
	checker := OwnerBasedChecker(OwnerBasedConfig{
		OwnerGetter: func(ctx context.Context, r *http.Request) ([]string, error) {
			return []string{"user-1"}, nil
		},
	})
	mw := Permission(PermissionConfig{Checker: checker})

	claims := &permClaims{subject: "user-99"}
	handler := withContextValue("claims", claims)(mw(okHandler))

	w := do(handler, http.MethodGet, "/resource/1", nil)
	if !containsString(w.Body.String(), `"code":403`) {
		t.Errorf("non-owner should be forbidden, got: %s", w.Body.String())
	}
}

func TestOwnerBasedChecker_SkipRoles(t *testing.T) {
	checker := OwnerBasedChecker(OwnerBasedConfig{
		SkipRoles: []string{"admin"},
		OwnerGetter: func(ctx context.Context, r *http.Request) ([]string, error) {
			return []string{}, nil // 没有所有者
		},
	})
	mw := Permission(PermissionConfig{Checker: checker})

	// admin 跳过所有权检查
	claims := &permClaims{subject: "admin-1", roles: []string{"admin"}}
	handler := withContextValue("claims", claims)(mw(okHandler))

	w := do(handler, http.MethodGet, "/resource/1", nil)
	if w.Code != http.StatusOK {
		t.Errorf("admin skip role should pass, status = %d", w.Code)
	}
}

// ============================================================================
// Gin 集成测试
// ============================================================================

func TestPermission_Gin(t *testing.T) {
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return nil
	})
	mw := Permission(PermissionConfig{Checker: checker})
	r := ginEngine(http.MethodGet, "/secure", mw, okHandler)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[Gin] status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestPermission_Gin_Deny(t *testing.T) {
	checker := PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
		return ErrForbidden
	})
	mw := Permission(PermissionConfig{Checker: checker})
	r := ginEngine(http.MethodGet, "/admin", mw, okHandler)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !containsString(w.Body.String(), `"code":403`) {
		t.Errorf("[Gin] should be forbidden, got: %s", w.Body.String())
	}
}
