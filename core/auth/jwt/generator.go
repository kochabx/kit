package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Generator JWT 生成器
type Generator struct {
	config *Config
}

// NewGenerator 创建生成器
func NewGenerator(config *Config) (*Generator, error) {
	return &Generator{config: config}, nil
}

// Generate 生成 token
func (g *Generator) Generate(claims Claims, ttl time.Duration) (string, error) {
	now := time.Now()
	jti := uuid.New().String()

	if rc, ok := claims.(*jwt.RegisteredClaims); ok {
		if rc.ID == "" {
			rc.ID = jti
		}
		rc.IssuedAt = jwt.NewNumericDate(now)
		rc.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
		if g.config.Issuer != "" {
			rc.Issuer = g.config.Issuer
		}
		if len(g.config.Audience) > 0 {
			rc.Audience = g.config.Audience
		}
	} else if setter, ok := claims.(StandardClaimsSetter); ok {
		setter.SetStandardClaims(jti, now, now.Add(ttl), g.config.Issuer, g.config.Audience)
	}

	token := jwt.NewWithClaims(g.config.GetSigningMethod(), claims)
	return token.SignedString(g.config.GetSecret())
}

// Parse 解析 token
func (g *Generator) Parse(tokenString string, claims Claims) error {
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		// 验证签名方法
		if token.Method != g.config.GetSigningMethod() {
			return nil, ErrInvalidSignature
		}
		return g.config.GetSecret(), nil
	})

	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return ErrInvalidToken
	}

	return nil
}

// Validate 仅验证 token 有效性
func (g *Generator) Validate(tokenString string) error {
	claims := &RegisteredClaims{}
	return g.Parse(tokenString, claims)
}
