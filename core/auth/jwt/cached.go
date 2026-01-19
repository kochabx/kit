package jwt

import (
	"context"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/auth/jwt/cache"
)

// CachedAuthenticator 带缓存的认证器
type CachedAuthenticator struct {
	basic        Authenticator
	sessionStore cache.SessionStore
	blacklist    cache.Blacklist
	config       *CacheConfig
}

// NewCachedAuthenticator 创建带缓存的认证器
func NewCachedAuthenticator(
	basic Authenticator,
	sessionStore cache.SessionStore,
	blacklist cache.Blacklist,
	opts ...CacheOption,
) *CachedAuthenticator {
	config := &CacheConfig{}

	for _, opt := range opts {
		opt(config)
	}

	return &CachedAuthenticator{
		basic:        basic,
		sessionStore: sessionStore,
		blacklist:    blacklist,
		config:       config,
	}
}

// Generate 生成 token 对（带会话管理）
func (a *CachedAuthenticator) Generate(ctx context.Context, claims Claims, opts ...GenerateOption) (*TokenPair, error) {
	// 解析选项
	options := &GenerateOptions{}
	for _, opt := range opts {
		opt(options)
	}

	subject, _ := claims.GetSubject()

	// 检查设备限制
	if a.config.MultiLogin && a.config.MaxDevices > 0 {
		if err := a.enforceMaxDevices(ctx, subject, options.DeviceID); err != nil {
			return nil, err
		}
	}

	// 调用基础认证器生成 token
	tokenPair, err := a.basic.Generate(ctx, claims, opts...)
	if err != nil {
		return nil, err
	}

	// 保存会话信息
	if err := a.saveSession(ctx, tokenPair, claims, options); err != nil {
		// 会话保存失败不影响 token 生成，记录错误即可
		return tokenPair, nil
	}

	return tokenPair, nil
}

// Verify 验证 token（检查黑名单和会话）
func (a *CachedAuthenticator) Verify(ctx context.Context, tokenString string, claims Claims) error {
	// 基础验证
	if err := a.basic.Verify(ctx, tokenString, claims); err != nil {
		return err
	}

	// 获取 JTI（通过类型断言）
	var jti string
	if rc, ok := claims.(*RegisteredClaims); ok {
		jti = rc.ID
	} else if sub, err := claims.GetSubject(); err == nil {
		// 如果无法直接获取 ID，尝试从嵌入的 RegisteredClaims 获取
		if rc := getRegisteredClaimsFromClaims(claims); rc != nil {
			jti = rc.ID
		}
		_ = sub // 避免未使用变量警告
	}

	// 检查黑名单
	if revoked, err := a.blacklist.Contains(ctx, jti); err != nil {
		return fmt.Errorf("check blacklist: %w", err)
	} else if revoked {
		return ErrTokenRevoked
	}

	// 验证会话存在
	if _, err := a.sessionStore.GetSession(ctx, jti); err != nil {
		if err == cache.ErrSessionNotFound {
			return ErrSessionNotFound
		}
		return fmt.Errorf("get session: %w", err)
	}

	return nil
}

// Refresh 刷新 token（保持会话信息）
func (a *CachedAuthenticator) Refresh(ctx context.Context, refreshToken string, claims Claims) (*TokenPair, error) {
	// 验证 refresh token
	if err := a.Verify(ctx, refreshToken, claims); err != nil {
		return nil, fmt.Errorf("verify refresh token: %w", err)
	}

	// 获取 JTI
	var jti string
	if rc, ok := claims.(*RegisteredClaims); ok {
		jti = rc.ID
	} else if rc := getRegisteredClaimsFromClaims(claims); rc != nil {
		jti = rc.ID
	}

	// 获取原会话信息
	session, err := a.sessionStore.GetSession(ctx, jti)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	if session.TokenType != "refresh" {
		return nil, ErrInvalidSession
	}

	// 生成新 token（保持设备信息）
	newPair, err := a.Generate(ctx, claims,
		WithDeviceID(session.DeviceID),
	)
	if err != nil {
		return nil, err
	}

	// 撤销旧的 refresh token
	if err := a.Revoke(ctx, refreshToken); err != nil {
		// 撤销失败不影响新 token，记录即可
		return newPair, nil
	}

	return newPair, nil
}

// Revoke 撤销单个 token
func (a *CachedAuthenticator) Revoke(ctx context.Context, tokenString string) error {
	claims := &RegisteredClaims{}
	if err := a.basic.Verify(ctx, tokenString, claims); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}

	jti := claims.ID

	// 获取过期时间
	exp, _ := claims.GetExpirationTime()
	var ttl time.Duration
	if exp != nil {
		ttl = time.Until(exp.Time)
		if ttl < 0 {
			ttl = 0
		}
	}

	// 添加到黑名单
	if err := a.blacklist.Add(ctx, jti, ttl); err != nil {
		return fmt.Errorf("add to blacklist: %w", err)
	}

	// 删除会话
	if err := a.sessionStore.DeleteSession(ctx, jti); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

// RevokeAll 撤销用户所有 token
func (a *CachedAuthenticator) RevokeAll(ctx context.Context, subject string) error {
	sessions, err := a.sessionStore.ListSessions(ctx, subject)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	for _, session := range sessions {
		ttl := time.Until(session.ExpiresAt)
		if ttl < 0 {
			ttl = 0
		}

		if err := a.blacklist.Add(ctx, session.JTI, ttl); err != nil {
			return fmt.Errorf("add to blacklist: %w", err)
		}
	}

	if err := a.sessionStore.DeleteAllSessions(ctx, subject); err != nil {
		return fmt.Errorf("delete all sessions: %w", err)
	}

	return nil
}

// ListSessions 列出用户所有会话
func (a *CachedAuthenticator) ListSessions(ctx context.Context, subject string) ([]*cache.Session, error) {
	return a.sessionStore.ListSessions(ctx, subject)
}

// RevokeDevice 撤销指定设备的所有会话
func (a *CachedAuthenticator) RevokeDevice(ctx context.Context, subject, deviceID string) error {
	sessions, err := a.sessionStore.ListSessions(ctx, subject)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	for _, session := range sessions {
		if session.DeviceID == deviceID {
			ttl := time.Until(session.ExpiresAt)
			if ttl < 0 {
				ttl = 0
			}

			if err := a.blacklist.Add(ctx, session.JTI, ttl); err != nil {
				return fmt.Errorf("add to blacklist: %w", err)
			}

			if err := a.sessionStore.DeleteSession(ctx, session.JTI); err != nil {
				return fmt.Errorf("delete session: %w", err)
			}
		}
	}

	return nil
}

// saveSession 保存会话信息
func (a *CachedAuthenticator) saveSession(ctx context.Context, tokenPair *TokenPair, claims Claims, options *GenerateOptions) error {
	// 需要重新解析 token 以获取完整的 JTI 和过期时间
	accessClaims := &RegisteredClaims{}
	refreshClaims := &RegisteredClaims{}

	// 解析 access token
	if basicAuth, ok := a.basic.(*BasicAuthenticator); ok {
		if err := basicAuth.generator.Parse(tokenPair.AccessToken, accessClaims); err != nil {
			return fmt.Errorf("parse access token: %w", err)
		}

		if err := basicAuth.generator.Parse(tokenPair.RefreshToken, refreshClaims); err != nil {
			return fmt.Errorf("parse refresh token: %w", err)
		}
	} else {
		// 如果不是 BasicAuthenticator，直接验证
		if err := a.basic.Verify(ctx, tokenPair.AccessToken, accessClaims); err != nil {
			return fmt.Errorf("verify access token: %w", err)
		}
		if err := a.basic.Verify(ctx, tokenPair.RefreshToken, refreshClaims); err != nil {
			return fmt.Errorf("verify refresh token: %w", err)
		}
	}

	now := time.Now()
	subject, _ := claims.GetSubject()

	// 保存 access token 会话
	accessSession := &cache.Session{
		JTI:       accessClaims.ID,
		Subject:   subject,
		TokenType: "access",
		CreatedAt: now,
		ExpiresAt: accessClaims.ExpiresAt.Time,
		DeviceID:  options.DeviceID,
	}

	if err := a.sessionStore.SaveSession(ctx, accessSession); err != nil {
		return fmt.Errorf("save access session: %w", err)
	}

	// 保存 refresh token 会话
	refreshSession := &cache.Session{
		JTI:       refreshClaims.ID,
		Subject:   subject,
		TokenType: "refresh",
		CreatedAt: now,
		ExpiresAt: refreshClaims.ExpiresAt.Time,
		DeviceID:  options.DeviceID,
	}

	if err := a.sessionStore.SaveSession(ctx, refreshSession); err != nil {
		return fmt.Errorf("save refresh session: %w", err)
	}

	return nil
}

// enforceMaxDevices 强制设备数限制
func (a *CachedAuthenticator) enforceMaxDevices(ctx context.Context, subject, newDeviceID string) error {
	sessions, err := a.sessionStore.ListSessions(ctx, subject)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	// 统计设备
	devices := make(map[string]*cache.Session)
	for _, session := range sessions {
		if session.DeviceID != "" {
			if existing, ok := devices[session.DeviceID]; !ok || session.CreatedAt.Before(existing.CreatedAt) {
				devices[session.DeviceID] = session
			}
		}
	}

	// 如果新设备已存在，不需要检查
	if _, exists := devices[newDeviceID]; exists {
		return nil
	}

	// 检查是否超限
	if len(devices) >= a.config.MaxDevices {
		// 找最旧的设备并删除
		var oldestDevice string
		var oldestSession *cache.Session
		for deviceID, session := range devices {
			if oldestSession == nil || session.CreatedAt.Before(oldestSession.CreatedAt) {
				oldestDevice = deviceID
				oldestSession = session
			}
		}

		if oldestDevice != "" {
			if err := a.RevokeDevice(ctx, subject, oldestDevice); err != nil {
				return fmt.Errorf("revoke oldest device: %w", err)
			}
		}
	}

	return nil
}

// getRegisteredClaimsFromClaims 从 Claims 接口提取 RegisteredClaims
func getRegisteredClaimsFromClaims(claims Claims) *RegisteredClaims {
	if rc, ok := claims.(*RegisteredClaims); ok {
		return rc
	}
	// 对于自定义类型，无法通过类型断言直接获取
	// 用户应该直接访问嵌入的字段
	return nil
}
