# JWT 认证模块

本模块提供了完整的 JWT 认证解决方案，包括 Token 生成、解析、验证以及基于 Redis 的缓存管理功能。

## 特性

- 🔐 **完整的 JWT 支持**: 支持多种签名算法 (HS256, HS384, HS512, RS256, ES256 等)
- ⚡ **Redis 缓存集成**: 提供高性能的 Token 缓存和会话管理
- 🖥️ **多设备登录控制**: 支持单点登录和多点登录模式
- 🔄 **Token 刷新机制**: 自动处理 Access Token 和 Refresh Token
- 🛡️ **会话安全**: 支持设备限制、Token 撤销等安全特性
- 📊 **元数据追踪**: 记录设备 ID、IP 地址、用户代理等信息

## 快速开始

### 基础 JWT 使用

```go
package main

import (
    "fmt"
    "github.com/golang-jwt/jwt/v5"
    "github.com/kochabx/kit/core/auth/jwt"
)

// 定义自定义Claims
type UserClaims struct {
    UserID   int64  `json:"user_id"`
    Username string `json:"username"`
    Role     string `json:"role"`
    Email    string `json:"email"`
    jwt.RegisteredClaims
}

// 实现ClaimsWithUserID接口
func (c *UserClaims) GetUserID() int64 {
    return c.UserID
}

func main() {
    // 创建JWT实例
    jwtInstance, err := jwt.New(&jwt.Config{
        Secret:        "your-secret-key",
        SigningMethod: "HS256",
        Expire:        3600,          // 1小时
        RefreshExpire: 604800,        // 7天
        Issuer:        "your-app",
        Audience:      "your-users",
    })
    if err != nil {
        panic(err)
    }

    // 创建Claims
    claims := &UserClaims{
        UserID:   12345,
        Username: "john_doe",
        Role:     "user",
        Email:    "john@example.com",
    }

    // 生成Token
    accessToken, err := jwtInstance.Generate(claims)
    if err != nil {
        panic(err)
    }
    fmt.Println("Access Token:", accessToken)

    // 生成Refresh Token
    refreshToken, err := jwtInstance.GenerateRefreshToken(claims)
    if err != nil {
        panic(err)
    }
    fmt.Println("Refresh Token:", refreshToken)

    // 解析Token
    var parsedClaims UserClaims
    err = jwtInstance.Parse(accessToken, &parsedClaims)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Parsed Claims: %+v\n", parsedClaims)
}
```

### 使用 Redis 缓存

```go
package main

import (
    "context"
    "fmt"
    "github.com/redis/go-redis/v9"
    "github.com/kochabx/kit/core/auth/jwt"
)

func main() {
    // 创建JWT实例
    jwtInstance, _ := jwt.New(&jwt.Config{
        Secret: "your-secret-key",
    })

    // 创建Redis客户端
    redisClient := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })

    // 创建JWT缓存实例
    jwtCache := jwt.NewJwtCache(
        jwtInstance,
        redisClient,
        jwt.WithKeyPrefix("myapp:jwt"),
        jwt.WithMultiLogin(true),      // 启用多点登录
        jwt.WithMaxDevices(3),         // 最多3个设备
    )

    ctx := context.Background()

    // 生成带缓存的Token
    claims := &UserClaims{
        UserID:   12345,
        Username: "john_doe",
    }

    auth, err := jwtCache.Generate(ctx, claims, &jwt.TokenGenerationMetadata{
        DeviceID:  "device_123",
        IPAddress: "192.168.1.100",
        UserAgent: "MyApp/1.0",
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("Access Token: %s\n", auth.AccessToken)
    fmt.Printf("Refresh Token: %s\n", auth.RefreshToken)

    // 验证Token
    var verifiedClaims UserClaims
    err = jwtCache.Parse(ctx, &verifiedClaims, auth.AccessToken)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Verified Claims: %+v\n", verifiedClaims)
}
```

## 配置选项

### JWT 配置

```go
type Config struct {
    Secret        string `json:"secret"`         // 签名密钥
    SigningMethod string `json:"signingMethod"`  // 签名算法
    Expire        int64  `json:"expire"`         // Access Token过期时间(秒)
    RefreshExpire int64  `json:"refreshExpire"`  // Refresh Token过期时间(秒)
    IssuedAt      int64  `json:"issuedAt"`       // 签发时间偏移
    NotBefore     int64  `json:"notBefore"`      // 生效时间偏移
    Audience      string `json:"audience"`       // 受众
    Issuer        string `json:"issuer"`         // 签发者
    Subject       string `json:"subject"`        // 主题
}
```

**默认值:**

- `Secret`: `"73c9e9e1-e606-4ed5-bceb-840a3d93c152"`
- `SigningMethod`: `"HS256"`
- `Expire`: `3600` (1 小时)
- `RefreshExpire`: `604800` (7 天)

### 支持的签名算法

- **HMAC**: HS256, HS384, HS512
- **RSA**: RS256, RS384, RS512
- **ECDSA**: ES256, ES384, ES512
- **RSA-PSS**: PS256, PS384, PS512

### JWT 缓存配置

```go
// 配置选项
jwt.WithKeyPrefix("myapp:jwt")    // Redis键前缀
jwt.WithMultiLogin(true)          // 启用多点登录
jwt.WithMaxDevices(5)             // 最大设备数限制
```

## 核心 API

### JWT 基础操作

#### 生成 Token

```go
// 生成Access Token
token, err := jwtInstance.Generate(claims)

// 生成Refresh Token
refreshToken, err := jwtInstance.GenerateRefreshToken(claims)

// 生成带JTI的Token
token, err := jwtInstance.GenerateWithJTI(claims, "unique-jti")
```

#### 解析和验证 Token

```go
// 解析Token到Claims
var claims UserClaims
err := jwtInstance.Parse(tokenString, &claims)

// 仅验证Token有效性
err := jwtInstance.Validate(tokenString)

// 泛型解析
claims, err := jwt.ParseClaims(jwtInstance, tokenString, &UserClaims{})
```

#### 提取 JTI

```go
jti, err := jwtInstance.GetJTI(claims)
```

### JWT 缓存操作

#### 生成和缓存 Token

```go
auth, err := jwtCache.Generate(ctx, claims, &jwt.TokenGenerationMetadata{
    DeviceID:  "device_001",
    IPAddress: "192.168.1.100",
    UserAgent: "Mozilla/5.0...",
})
```

#### 解析和验证缓存 Token

```go
var claims UserClaims
err := jwtCache.Parse(ctx, &claims, accessToken)

// 同时验证多个Token
err := jwtCache.Parse(ctx, &claims, accessToken, refreshToken)
```

#### 会话管理

```go
// 获取用户Token元数据
metadata, err := jwtCache.GetUserTokenMetadata(ctx, userID)

// 获取活跃会话数
count, err := jwtCache.GetActiveSessionCount(ctx, userID)

// 获取活跃设备数
devices, err := jwtCache.GetActiveDevicesCount(ctx, userID)

// 删除用户所有Token
err := jwtCache.DeleteUserTokens(ctx, userID)

// 撤销特定Token
err := jwtCache.RevokeToken(ctx, tokenString, &claims)
```

## 自定义 Claims

实现自定义 Claims 时，需要嵌入`jwt.RegisteredClaims`并实现`ClaimsWithUserID`接口：

```go
type CustomClaims struct {
    UserID      int64    `json:"user_id"`
    Username    string   `json:"username"`
    Roles       []string `json:"roles"`
    Permissions []string `json:"permissions"`
    TenantID    string   `json:"tenant_id"`
    jwt.RegisteredClaims
}

func (c *CustomClaims) GetUserID() int64 {
    return c.UserID
}
```

## 登录模式

### 单点登录 (SingleLogin)

- 用户只能在一个设备上保持登录状态
- 新登录会使之前的 Token 失效
- 适用于高安全要求的场景

```go
jwtCache := jwt.NewJwtCache(
    jwtInstance,
    redisClient,
    jwt.WithMultiLogin(false), // 禁用多点登录
)
```

### 多点登录 (MultiLogin)

- 用户可以在多个设备上同时登录
- 支持设备数量限制
- 支持按设备管理会话

```go
jwtCache := jwt.NewJwtCache(
    jwtInstance,
    redisClient,
    jwt.WithMultiLogin(true),  // 启用多点登录
    jwt.WithMaxDevices(3),     // 最多3个设备
)
```

## Token 元数据

系统会自动记录以下 Token 元数据：

```go
type TokenMetadata struct {
    JTI       string `json:"jti"`         // Token唯一标识
    TokenType string `json:"token_type"`  // "access" 或 "refresh"
    CreatedAt int64  `json:"created_at"`  // 创建时间
    ExpiresAt int64  `json:"expires_at"`  // 过期时间
    DeviceID  string `json:"device_id"`   // 设备ID
    IPAddress string `json:"ip_address"`  // IP地址
    UserAgent string `json:"user_agent"`  // 用户代理
}
```

## Redis 存储结构

### 单点登录模式

```
jwt:user_tokens:{userID} -> Hash {
    "access_token":  TokenMetadata JSON
    "refresh_token": TokenMetadata JSON
}

jwt:jti_mapping:{jti} -> "{userID}"
```

### 多点登录模式

```
jwt:user_tokens:{userID} -> Hash {
    "{deviceID}_{timestamp}": SessionData JSON
}

jwt:jti_mapping:{jti} -> "{userID}:{sessionID}"
```

## 安全特性

### 自动过期清理

- Redis TTL 自动清理过期 Token
- JTI 映射同步过期

### 设备限制

- 支持最大设备数限制
- 自动清理最旧的设备会话

### Token 撤销

- 支持单个 Token 撤销
- 支持用户所有 Token 撤销

## 最佳实践

### 1. 安全配置

```go
config := &jwt.Config{
    Secret:        generateSecureSecret(), // 使用强随机密钥
    SigningMethod: "HS256",               // 生产环境推荐HS256或RS256
    Expire:        900,                   // 15分钟Access Token
    RefreshExpire: 86400,                 // 1天Refresh Token
    Issuer:        "your-service-name",
    Audience:      "your-app-users",
}
```

### 2. 错误处理

```go
err := jwtCache.Parse(ctx, &claims, token)
switch {
case errors.Is(err, jwt.ErrTokenExpired):
    // Token过期，需要刷新
case errors.Is(err, jwt.ErrTokenNotValidYet):
    // Token尚未生效
case err != nil:
    // 其他错误
}
```

### 3. 会话管理

```go
// 登录时
auth, err := jwtCache.Generate(ctx, claims, &jwt.TokenGenerationMetadata{
    DeviceID:  getDeviceID(request),
    IPAddress: getClientIP(request),
    UserAgent: request.UserAgent(),
})

// 登出时
err = jwtCache.RevokeToken(ctx, token, &claims)

// 强制登出所有设备
err = jwtCache.DeleteUserTokens(ctx, userID)
```

## 测试

运行测试：

```bash
go test ./core/auth/jwt
```

运行基准测试：

```bash
go test -bench=. ./core/auth/jwt
```

## 注意事项

1. **密钥安全**: 生产环境中务必使用强随机密钥
2. **Token 时效**: 合理设置 Token 过期时间，平衡安全性和用户体验
3. **Redis 连接**: 确保 Redis 连接的高可用性
4. **Claims 设计**: Claims 中不要包含敏感信息
5. **JTI 唯一性**: 确保 JTI 的全局唯一性

## 依赖

- `github.com/golang-jwt/jwt/v5`: JWT 库
- `github.com/redis/go-redis/v9`: Redis 客户端
- `github.com/google/uuid`: UUID 生成