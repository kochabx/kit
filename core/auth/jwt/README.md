# JWT 认证模块

`core/auth/jwt` 提供接口驱动的 JWT 认证能力，支持纯 JWT 认证器和带缓存的认证器。缓存能力通过接口解耦，Redis 支持作为可选 adapter 放在 `core/auth/jwt/cache/redis`。

## 特性

- 接口驱动：核心接口是 `Authenticator`，便于替换实现和测试。
- 纯 JWT 与缓存解耦：`BasicAuthenticator` 负责 token 生成、验证和刷新，`CachedAuthenticator` 通过 `SessionStore` 与 `Blacklist` 增加会话管理。
- 可选 Redis adapter：只有需要 Redis 会话存储时才引入 `core/auth/jwt/cache/redis`。
- 支持标准 JWT claims：可直接使用或嵌入 `jwt.RegisteredClaims`。
- 支持多种签名算法：HS、RS、ES、PS 系列算法。
- Functional Options 配置：支持代码配置，也支持从配置文件绑定到 `Config`。

## 安装

```bash
go get github.com/kochabx/kit/core/auth/jwt
```

如果使用 Redis 缓存适配器，需要同时引入 Redis 客户端：

```bash
go get github.com/redis/go-redis/v9
```

## 纯 JWT 使用

适用于无状态 API 认证场景。

```go
package main

import (
	"context"
	"fmt"

	"github.com/kochabx/kit/core/auth/jwt"
)

type UserClaims struct {
	jwt.RegisteredClaims
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func main() {
	ctx := context.Background()

	auth, err := jwt.NewBasicAuthenticator(
		jwt.WithSecret("your-secret-key"),
		jwt.WithAccessTokenTTL(3600),
		jwt.WithRefreshTokenTTL(604800),
		jwt.WithIssuer("my-app"),
	)
	if err != nil {
		panic(err)
	}

	claims := &UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user123",
		},
		UserID:   123,
		Username: "john",
		Role:     "admin",
	}

	tokenPair, err := auth.Generate(ctx, claims)
	if err != nil {
		panic(err)
	}

	verifiedClaims := &UserClaims{}
	if err := auth.Verify(ctx, tokenPair.AccessToken, verifiedClaims); err != nil {
		panic(err)
	}

	fmt.Printf("user=%s role=%s\n", verifiedClaims.Username, verifiedClaims.Role)

	newPair, err := auth.Refresh(ctx, tokenPair.RefreshToken, verifiedClaims)
	if err != nil {
		panic(err)
	}

	fmt.Println("new access token:", newPair.AccessToken)
}
```

## Redis 缓存认证器

适用于需要会话管理、Token 撤销和多设备控制的场景。

Redis 支持位于 `core/auth/jwt/cache/redis`，是可选 adapter。JWT 核心只依赖 `core/auth/jwt/cache` 中的接口；只有使用 Redis 会话存储时才需要引入该子包和 Redis 客户端。

```go
package main

import (
	"context"

	"github.com/kochabx/kit/core/auth/jwt"
	"github.com/kochabx/kit/core/auth/jwt/cache/redis"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	basicAuth, err := jwt.NewBasicAuthenticator(jwt.WithSecret("secret"))
	if err != nil {
		panic(err)
	}

	rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	store := redis.NewStore(rdb, redis.WithStoreKeyPrefix("myapp:jwt"))

	cachedAuth := jwt.NewCachedAuthenticator(
		basicAuth,
		store.SessionStore,
		store.Blacklist,
		jwt.WithMultiLogin(5),
	)

	claims := &jwt.RegisteredClaims{Subject: "user123"}
	tokenPair, err := cachedAuth.Generate(ctx, claims, jwt.WithDeviceID("device-001"))
	if err != nil {
		panic(err)
	}

	verifiedClaims := &jwt.RegisteredClaims{}
	if err := cachedAuth.Verify(ctx, tokenPair.AccessToken, verifiedClaims); err != nil {
		panic(err)
	}

	sessions, err := cachedAuth.ListSessions(ctx, "user123")
	if err != nil {
		panic(err)
	}
	_ = sessions

	_ = cachedAuth.Revoke(ctx, tokenPair.AccessToken)
	_ = cachedAuth.RevokeAll(ctx, "user123")
	_ = cachedAuth.RevokeDevice(ctx, "user123", "device-001")
}
```

## 配置文件绑定

```yaml
jwt:
  secret: "your-secret-key"
  signingMethod: "HS256"
  accessTokenTTL: 3600
  refreshTokenTTL: 604800
  issuer: "my-app"
  audience:
    - "web"
    - "mobile"
```

```go
package main

import "github.com/kochabx/kit/core/auth/jwt"

func main() {
	cfg := &jwt.Config{
		Secret:          "your-secret-key",
		SigningMethod:   "HS256",
		AccessTokenTTL:  3600,
		RefreshTokenTTL: 604800,
		Issuer:          "my-app",
		Audience:        []string{"web", "mobile"},
	}

	auth, err := jwt.New(cfg)
	if err != nil {
		panic(err)
	}

	_ = auth
}
```

## API

### Authenticator

```go
type Authenticator interface {
	Generate(ctx context.Context, claims Claims, opts ...GenerateOption) (*TokenPair, error)
	Verify(ctx context.Context, tokenString string, claims Claims) error
	Refresh(ctx context.Context, refreshToken string, claims Claims) (*TokenPair, error)
}
```

### BasicAuthenticator

```go
auth, err := jwt.NewBasicAuthenticator(opts ...Option)
auth, err := jwt.New(config *Config)
```

### CachedAuthenticator

```go
cachedAuth := jwt.NewCachedAuthenticator(
	basic Authenticator,
	sessionStore cache.SessionStore,
	blacklist cache.Blacklist,
	opts ...CacheOption,
)

err := cachedAuth.Revoke(ctx, tokenString)
err := cachedAuth.RevokeAll(ctx, subject)
err := cachedAuth.RevokeDevice(ctx, subject, deviceID)
sessions, err := cachedAuth.ListSessions(ctx, subject)
```

### Config

```go
type Config struct {
	Secret          string   `json:"secret" validate:"required"`
	SigningMethod   string   `json:"signingMethod" default:"HS256"`
	AccessTokenTTL  int64    `json:"accessTokenTTL" default:"3600" validate:"gt=0"`
	RefreshTokenTTL int64    `json:"refreshTokenTTL" default:"604800" validate:"gt=0"`
	Issuer          string   `json:"issuer"`
	Audience        []string `json:"audience"`
}
```

### Options

```go
jwt.WithSecret(secret)
jwt.WithAccessTokenTTL(seconds)
jwt.WithRefreshTokenTTL(seconds)
jwt.WithSigningMethod(method)
jwt.WithIssuer(issuer)
jwt.WithAudience(audience...)

jwt.WithDeviceID(deviceID)
jwt.WithMultiLogin(maxDevices)
jwt.WithMaxDevices(maxDevices)
```

## Redis 存储结构

```text
jwt:session:{jti} -> Session JSON
jwt:subject:{subject} -> Set[jti1, jti2, ...]
jwt:blacklist:{jti} -> "1"
```

## 架构说明

```text
Authenticator
├── BasicAuthenticator
└── CachedAuthenticator
    ├── cache.SessionStore
    └── cache.Blacklist
```

`BasicAuthenticator` 只处理 JWT 本身，`CachedAuthenticator` 通过装饰 `BasicAuthenticator` 增加会话与撤销能力。Redis 只是 `cache.SessionStore` 与 `cache.Blacklist` 的一个实现，不是 JWT 核心包的必需依赖。

## 安全建议

- 生产环境使用强随机密钥，并通过环境变量或密钥管理服务注入。
- Access Token 使用较短 TTL，Refresh Token 根据业务风险设置较长 TTL。
- 登出、修改密码、权限变更等场景应调用撤销能力。
- 生产环境必须使用 HTTPS 传输 token。
- Claims 中不要放密码、密钥、证件号等敏感信息。

## 测试

```bash
go test ./core/auth/jwt/...
go test -bench=. ./core/auth/jwt
go test -cover ./core/auth/jwt/...
```
