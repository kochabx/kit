package jwt

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/kochabx/kit/core/stag"
)

// Config holds JWT configuration
type Config struct {
	Secret        string `json:"secret" default:"jwt"`
	SigningMethod string `json:"signingMethod" default:"HS256"`
	Expire        int64  `json:"expire" default:"3600"`          // 3600 seconds
	RefreshExpire int64  `json:"refreshExpire" default:"604800"` // 7 days
	IssuedAt      int64  `json:"issuedAt"`
	NotBefore     int64  `json:"notBefore"`
	Audience      string `json:"audience"`
	Issuer        string `json:"issuer"`
	Subject       string `json:"subject"`
}

func (c *Config) init() error {
	return stag.ApplyDefaults(c)
}

func (c *Config) signingMethod() jwt.SigningMethod {
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
