package jwt

import "errors"

var (
	// Token 相关错误
	ErrInvalidToken     = errors.New("jwt: invalid token")
	ErrExpiredToken     = errors.New("jwt: token expired")
	ErrTokenRevoked     = errors.New("jwt: token revoked")
	ErrInvalidSignature = errors.New("jwt: invalid signature")
	ErrInvalidClaims    = errors.New("jwt: invalid claims")

	// 配置相关错误
	ErrConfigInvalid = errors.New("jwt: invalid configuration")
	ErrEmptySecret   = errors.New("jwt: secret cannot be empty")

	// 会话相关错误
	ErrSessionNotFound = errors.New("jwt: session not found")
	ErrMaxDevicesLimit = errors.New("jwt: max devices limit reached")
	ErrInvalidSession  = errors.New("jwt: invalid session")
)
