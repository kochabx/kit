package jwt

import "errors"

var (
	// Token errors.
	ErrInvalidToken     = errors.New("invalid token")
	ErrExpiredToken     = errors.New("token expired")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrInvalidClaims    = errors.New("invalid claims")
	ErrInvalidIssuer    = errors.New("invalid issuer")
	ErrInvalidAudience  = errors.New("invalid audience")
	ErrTokenTooLarge    = errors.New("token too large")

	// Configuration errors.
	ErrConfigInvalid = errors.New("invalid configuration")

	// Session errors.
	ErrSessionNotFound    = errors.New("session not found")
	ErrInvalidSession     = errors.New("invalid session")
	ErrSessionRevoked     = errors.New("session revoked")
	ErrRefreshReused      = errors.New("refresh token reused")
	ErrRefreshRateLimited = errors.New("refresh rate limited")
)
