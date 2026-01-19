package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT Claims 接口类型别名
// 用户可以直接使用本包的 Claims 类型，无需导入 jwt/v5
type Claims = jwt.Claims

// RegisteredClaims JWT 标准 Claims 类型别名
// 用户可以通过嵌入本类型来创建自定义 claims
type RegisteredClaims = jwt.RegisteredClaims

// StandardClaimsSetter 可选接口，用于自动设置标准字段
// 嵌入 RegisteredClaims 的自定义类型可以实现此接口
type StandardClaimsSetter interface {
	Claims
	// SetStandardClaims 设置标准字段（JTI、时间等）
	SetStandardClaims(jti string, issuedAt, expiresAt time.Time, issuer string, audience []string)
}
