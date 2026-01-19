package jwt

import (
	"context"
	"fmt"

	"github.com/kochabx/kit/core/tag"
)

// BasicAuthenticator 基础认证器
type BasicAuthenticator struct {
	generator *Generator
	config    *Config
}

// NewBasicAuthenticator 创建基础认证器
func NewBasicAuthenticator(opts ...Option) (*BasicAuthenticator, error) {
	config := &Config{}
	if err := tag.ApplyDefaults(config); err != nil {
		return nil, fmt.Errorf("apply defaults: %w", err)
	}

	for _, opt := range opts {
		opt(config)
	}

	generator, err := NewGenerator(config)
	if err != nil {
		return nil, err
	}

	return &BasicAuthenticator{
		generator: generator,
		config:    config,
	}, nil
}

// New 从配置创建基础认证器
func New(config *Config) (*BasicAuthenticator, error) {
	if err := tag.ApplyDefaults(config); err != nil {
		return nil, fmt.Errorf("apply defaults: %w", err)
	}

	generator, err := NewGenerator(config)
	if err != nil {
		return nil, err
	}

	return &BasicAuthenticator{
		generator: generator,
		config:    config,
	}, nil
}

// Generate 生成 token 对
func (a *BasicAuthenticator) Generate(ctx context.Context, claims Claims, opts ...GenerateOption) (*TokenPair, error) {
	// 生成 Access Token（JTI 会在 generator 中自动设置）
	accessToken, err := a.generator.Generate(claims, a.config.GetAccessTokenTTL())
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	// 生成 Refresh Token（JTI 会在 generator 中自动设置）
	refreshToken, err := a.generator.Generate(claims, a.config.GetRefreshTokenTTL())
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    a.config.AccessTokenTTL,
	}, nil
}

// Verify 验证 token
func (a *BasicAuthenticator) Verify(ctx context.Context, tokenString string, claims Claims) error {
	return a.generator.Parse(tokenString, claims)
}

// Refresh 刷新 token
func (a *BasicAuthenticator) Refresh(ctx context.Context, refreshToken string, claims Claims) (*TokenPair, error) {
	// 验证 refresh token
	if err := a.Verify(ctx, refreshToken, claims); err != nil {
		return nil, fmt.Errorf("verify refresh token: %w", err)
	}

	// 生成新的 token 对
	return a.Generate(ctx, claims)
}
