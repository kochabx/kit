package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config JWT 配置
type Config struct {
	// 必需配置
	Secret string `json:"secret" validate:"required"`

	// Token 配置
	SigningMethod   string `json:"signingMethod" default:"HS256"`
	AccessTokenTTL  int64  `json:"accessTokenTTL" default:"3600" validate:"gt=0"`    // 秒，默认 1 小时
	RefreshTokenTTL int64  `json:"refreshTokenTTL" default:"604800" validate:"gt=0"` // 秒，默认 7 天

	// 标准 Claims 配置
	Issuer   string   `json:"issuer"`
	Audience []string `json:"audience"`
}

// GetSigningMethod 获取签名方法
func (c *Config) GetSigningMethod() jwt.SigningMethod {
	switch c.SigningMethod {
	case "HS256":
		return jwt.SigningMethodHS256
	case "HS384":
		return jwt.SigningMethodHS384
	case "HS512":
		return jwt.SigningMethodHS512
	case "RS256":
		return jwt.SigningMethodRS256
	case "RS384":
		return jwt.SigningMethodRS384
	case "RS512":
		return jwt.SigningMethodRS512
	case "ES256":
		return jwt.SigningMethodES256
	case "ES384":
		return jwt.SigningMethodES384
	case "ES512":
		return jwt.SigningMethodES512
	case "PS256":
		return jwt.SigningMethodPS256
	case "PS384":
		return jwt.SigningMethodPS384
	case "PS512":
		return jwt.SigningMethodPS512
	default:
		return jwt.SigningMethodHS256
	}
}

// GetAccessTokenTTL 获取 Access Token TTL
func (c *Config) GetAccessTokenTTL() time.Duration {
	return time.Duration(c.AccessTokenTTL) * time.Second
}

// GetRefreshTokenTTL 获取 Refresh Token TTL
func (c *Config) GetRefreshTokenTTL() time.Duration {
	return time.Duration(c.RefreshTokenTTL) * time.Second
}

// GetSecret 获取密钥字节
func (c *Config) GetSecret() []byte {
	return []byte(c.Secret)
}
