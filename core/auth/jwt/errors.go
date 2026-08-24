package jwt

import "errors"

var (
	// Token 相关错误
	ErrInvalidToken     = errors.New("jwt: invalid token")
	ErrExpiredToken     = errors.New("jwt: token expired")
	ErrInvalidSignature = errors.New("jwt: invalid signature")
	ErrInvalidClaims    = errors.New("jwt: invalid claims")
	ErrInvalidIssuer    = errors.New("jwt: invalid issuer")
	ErrInvalidAudience  = errors.New("jwt: invalid audience")
	ErrTokenTooLarge    = errors.New("jwt: token too large")

	// 配置相关错误
	ErrConfigInvalid = errors.New("jwt: invalid configuration")

	// 会话相关错误
	ErrSessionNotFound    = errors.New("jwt: session not found")
	ErrInvalidSession     = errors.New("jwt: invalid session")
	ErrSessionRevoked     = errors.New("jwt: session revoked")
	ErrRefreshReused      = errors.New("jwt: refresh token reused")
	ErrRefreshRateLimited = errors.New("jwt: refresh rate limited")
)
