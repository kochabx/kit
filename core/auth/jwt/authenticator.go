package jwt

import "context"

// Authenticator JWT 认证器接口
type Authenticator interface {
	// Generate 生成 token 对
	Generate(ctx context.Context, claims Claims, opts ...GenerateOption) (*TokenPair, error)

	// Verify 验证 token
	Verify(ctx context.Context, tokenString string, claims Claims) error

	// Refresh 刷新 token
	Refresh(ctx context.Context, refreshToken string, claims Claims) (*TokenPair, error)
}
