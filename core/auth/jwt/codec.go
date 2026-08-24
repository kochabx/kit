package jwt

import (
	"context"
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

type verifiedToken[T Claims] struct {
	claims T
	raw    string
}

type tokenKind uint8

const (
	accessToken tokenKind = iota
	refreshToken
)

type codec[T Claims] struct {
	keys            KeyProvider
	factory         ClaimsFactory[T]
	issuer          string
	accessAudience  []string
	refreshAudience []string
	leeway          time.Duration
	clock           Clock
	maxTokenBytes   int
}

func (c *codec[T]) sign(ctx context.Context, claims T) (string, error) {
	key, err := c.keys.SigningKey(ctx)
	if err != nil {
		return "", fmt.Errorf("signing key: %w", err)
	}
	token := gojwt.NewWithClaims(key.Method, claims)
	token.Header["kid"] = key.ID
	value, err := token.SignedString(key.Key)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	if len(value) > c.maxTokenBytes {
		return "", ErrTokenTooLarge
	}
	return value, nil
}

func (c *codec[T]) verify(ctx context.Context, raw string, kind tokenKind) (*verifiedToken[T], error) {
	if len(raw) == 0 {
		return nil, ErrInvalidToken
	}
	if len(raw) > c.maxTokenBytes {
		return nil, ErrTokenTooLarge
	}
	claims := c.factory()
	if isNil(claims) {
		return nil, ErrInvalidClaims
	}
	options := []gojwt.ParserOption{
		gojwt.WithExpirationRequired(),
		gojwt.WithLeeway(c.leeway),
		gojwt.WithTimeFunc(c.clock.Now),
	}
	if c.issuer != "" {
		options = append(options, gojwt.WithIssuer(c.issuer))
	}
	audience := c.accessAudience
	if kind == refreshToken {
		audience = c.refreshAudience
	}
	options = append(options, gojwt.WithAudience(audience...))
	token, err := gojwt.ParseWithClaims(raw, claims, func(token *gojwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		key, err := c.keys.VerificationKey(ctx, kid, token.Method.Alg())
		if err != nil {
			return nil, err
		}
		if token.Method.Alg() != key.Method.Alg() {
			return nil, ErrInvalidSignature
		}
		return key.Key, nil
	}, options...)
	if err != nil {
		if errors.Is(err, gojwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		if errors.Is(err, gojwt.ErrTokenSignatureInvalid) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidSignature, err)
		}
		if errors.Is(err, gojwt.ErrTokenInvalidIssuer) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidIssuer, err)
		}
		if errors.Is(err, gojwt.ErrTokenInvalidAudience) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidAudience, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	protocolClaims := claims.JWTClaims()
	if protocolClaims == nil {
		return nil, ErrInvalidClaims
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	rc := &protocolClaims.RegisteredClaims
	if rc.ID == "" || rc.Subject == "" || protocolClaims.SessionID == "" ||
		rc.IssuedAt == nil || rc.ExpiresAt == nil {
		return nil, ErrInvalidClaims
	}
	return &verifiedToken[T]{claims: claims, raw: raw}, nil
}
