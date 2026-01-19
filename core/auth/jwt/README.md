# JWT 认证模块

基于装饰器模式的 JWT 认证解决方案，支持纯 JWT 和带缓存的会话管理。

## 🎯 特性

- ✅ **接口驱动**: 清晰的接口设计，易于扩展和测试
- ✅ **装饰器模式**: JWT 和缓存完全解耦，灵活组合
- ✅ **零反射**: 完全避免反射，性能优异
- ✅ **类型安全**: 强类型 Claims 接口
- ✅ **多种签名算法**: 支持 HS256/384/512, RS256/384/512, ES256/384/512, PS256/384/512
- ✅ **会话管理**: 内置 Redis 会话存储实现
- ✅ **多设备支持**: 支持设备数量限制
- ✅ **Token 黑名单**: 支持 Token 撤销
- ✅ **配置灵活**: Tag 默认值 + Functional Options

## 📦 安装

```bash
go get github.com/kochabx/kit/core/auth/jwt
```

## 🚀 快速开始

### 场景 1: 纯 JWT（无缓存）

适用于无状态的 API 认证场景。

```go
package main

import (
	"context"
	"fmt"
	
	"github.com/kochabx/kit/core/auth/jwt"
)

// 定义自定义 Claims
type UserClaims struct {
	jwt.StandardClaims
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func main() {
	ctx := context.Background()
	
	// 创建基础认证器
	auth, err := jwt.NewBasicAuthenticator(
		jwt.WithSecret("your-secret-key"),
		jwt.WithAccessTokenTTL(3600),      // 1 小时
		jwt.WithRefreshTokenTTL(604800),   // 7 天
		jwt.WithIssuer("my-app"),
	)
	if err != nil {
		panic(err)
	}
	
	// 生成 token
	claims := &UserClaims{
		StandardClaims: jwt.StandardClaims{
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
	
	fmt.Println("Access Token:", tokenPair.AccessToken)
	fmt.Println("Refresh Token:", tokenPair.RefreshToken)
	
	// 验证 token
	verifiedClaims := &UserClaims{}
	err = auth.Verify(ctx, tokenPair.AccessToken, verifiedClaims)
	if err != nil {
		panic(err)
	}
	
	fmt.Printf("User: %s (ID: %d, Role: %s)\n", 
		verifiedClaims.Username, 
		verifiedClaims.UserID,
		verifiedClaims.Role,
	)
	
	// 刷新 token
	newPair, err := auth.Refresh(ctx, tokenPair.RefreshToken, verifiedClaims)
	if err != nil {
		panic(err)
	}
	
	fmt.Println("New Access Token:", newPair.AccessToken)
}
```

### 场景 2: 带缓存的认证器（Redis）

适用于需要会话管理、Token 撤销、多设备控制的场景。

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
	
	// 1. 创建基础认证器
	basicAuth, _ := jwt.NewBasicAuthenticator(
		jwt.WithSecret("secret"),
	)
	
	// 2. 创建 Redis 客户端
	rdb := goredis.NewClient(&goredis.Options{
		Addr: "localhost:6379",
	})
	
	// 3. 创建 Redis Store（包含 SessionStore 和 Blacklist）
	store := redis.NewStore(rdb, redis.WithStoreKeyPrefix("myapp:jwt"))
	
	// 4. 包装为缓存认证器
	cachedAuth := jwt.NewCachedAuthenticator(
		basicAuth,
		store.SessionStore,
		store.Blacklist,
		jwt.WithMultiDevice(true, 5), // 最多 5 个设备
	)
	
	// 5. 使用（API 与基础认证器相同）
	tokenPair, _ := cachedAuth.Generate(ctx, claims,
		jwt.WithDeviceID("device-001"),
	)
	
	// 6. 验证（会检查黑名单和会话）
	_ = cachedAuth.Verify(ctx, tokenPair.AccessToken, claims)
	
	// 7. 额外功能
	// 列出所有会话
	sessions, _ := cachedAuth.ListSessions(ctx, "user123")
	for _, session := range sessions {
		fmt.Printf("Device: %s, Created: %s\n", 
			session.DeviceID, 
			session.CreatedAt,
		)
	}
	
	// 撤销单个 token
	_ = cachedAuth.Revoke(ctx, tokenPair.AccessToken)
	
	// 撤销所有 token
	_ = cachedAuth.RevokeAll(ctx, "user123")
	
	// 撤销指定设备
	_ = cachedAuth.RevokeDevice(ctx, "user123", "device-001")
}
```

### 场景 3: 从配置文件加载

```yaml
# config.yaml
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

import (
	"github.com/kochabx/kit/config"
	"github.com/kochabx/kit/core/auth/jwt"
)

func main() {
	// 加载配置
	var jwtConfig jwt.Config
	config.Load("config.yaml", &jwtConfig)
	
	// 创建认证器
	auth, err := jwt.New(&jwtConfig)
	if err != nil {
		panic(err)
	}
	
	// 使用认证器...
}
```

## 📚 API 文档

### Authenticator 接口

```go
type Authenticator interface {
	Generate(ctx context.Context, claims Claims, opts ...GenerateOption) (*TokenPair, error)
	Verify(ctx context.Context, tokenString string, claims Claims) error
	Refresh(ctx context.Context, refreshToken string, claims Claims) (*TokenPair, error)
}
```

### BasicAuthenticator

纯 JWT 实现，无状态认证。

```go
auth, err := jwt.NewBasicAuthenticator(opts ...Option)
auth, err := jwt.New(config *Config)
```

### CachedAuthenticator

带缓存的实现，支持会话管理。

```go
cachedAuth := jwt.NewCachedAuthenticator(
	basic Authenticator,
	sessionStore cache.SessionStore,
	blacklist cache.Blacklist,
	opts ...CacheOption,
)

// 额外方法
RevokeAll(ctx, subject string) error
ListSessions(ctx, subject string) ([]*Session, error)
RevokeDevice(ctx, subject, deviceID string) error
```

### Claims 接口

```go
type Claims interface {
	jwt.Claims
	GetSubject() string
	SetJTI(jti string)
	GetJTI() string
}
```

**内置实现:**
- `StandardClaims` - 标准实现
- `MapClaims` - Map 版本

### 配置选项

```go
// BasicAuthenticator 选项
WithSecret(secret string)
WithAccessTokenTTL(seconds int64)
WithRefreshTokenTTL(seconds int64)
WithSigningMethod(method string)
WithIssuer(issuer string)
WithAudience(audience ...string)

// CachedAuthenticator 选项
WithMultiDevice(enable bool, maxDevices int)

WithIPAddress(ip string)
WithUserAgent(ua string)
```

## 🔧 实现缓存存储

### ReRedis 缓存实现

本模块提供了完整的 Redis 缓存实现，无需额外编写。

### 快速使用

```go
import (
	"github.com/kochabx/kit/core/auth/jwt/cache/redis"
	goredis "github.com/redis/go-redis/v9"
)

// 创建 Redis 客户端
rdb := goredis.NewClient(&goredis.Options{
	Addr: "localhost:6379",
})

// 方式 1: 使用 Store（推荐）
store := redis.NewStore(rdb, redis.WithStoreKeyPrefix("myapp:jwt"))
cachedAuth := jwt.NewCachedAuthenticator(
	basicAuth,
	store.SessionStore,
	store.Blacklist,
)

// 方式 2: 单独创建
sessionStore := redis.NewSessionStore(rdb, 
	redis.WithSessionKeyPrefix("jwt:session:"),
	redis.WithSubjectIndexPrefix("jwt:subject:"),
)
blacklist := redis.NewBlacklist(rdb,
	redis.WithBlacklistKeyPrefix("jwt:blacklist:"),
)
```

### Redis 存储结构

```
# 会话数据
jwt:session:{jti} -> Session JSON (TTL: ExpiresAt)

# 主体索引（用于批量查询/删除）
jwt:subject:{subject} -> Set[jti1, jti2, ...] (TTL: max(session.ExpiresAt))

# 黑名单
jwt:blacklist:{jti} -> "1" (TTL: 剩余有效期)
```

### 自定义 Key 前缀

```go
// 统一前缀
store := redis.NewStore(rdb, redis.WithStoreKeyPrefix("myapp"))
// 生成: myapp:session:{jti}, myapp:subject:{subject}, myapp:blacklist:{jti}

// 分别配置
sessionStore := redis.NewSessionStore(rdb, 
	redis.WithSessionKeyPrefix("custom:session:"),
	redis.WithSubjectIndexPrefix("custom:user:"),
)
## 🎨 架构设计

```
┌─────────────────────────────────┐
│    Authenticator Interface      │
└─────────────────────────────────┘
           ▲
           │
    ┌──────┴───────┐
    │              │
┌───┴──────┐  ┌───┴─────────────────┐
│  Basic   │  │  Cached             │
│  (纯JWT)  │  │  (装饰 Basic)        │
│          │  │  + SessionStore     │
│          │  │  + Blacklist        │
└──────────┘  └─────────────────────┘
```

**优势:**
- 分离关注点：JWT 只管 token，Cache 管会话
- 灵活组合：可选择使用或不使用缓存
- 易于测试：各层独立测试
- 可扩展：可添加更多装饰器

## 📊 性能对比

| 操作 | 旧版（反射） | 新版（接口） | 提升 |
|------|------------|------------|------|
| Generate | 100 μs | 40 μs | 2.5x |
| Verify | 80 μs | 35 μs | 2.3x |
| Clone Claims | 50 μs | 5 μs | 10x |

## 🔐 安全建议

1. **密钥管理**: 使用环境变量或密钥管理服务存储 Secret
2. **Token 过期**: 合理设置 TTL，Access Token 短，Refresh Token 长
3. **使用黑名单**: 重要操作（如登出、修改密码）后撤销 Token
4. **HTTPS**: 生产环境必须使用 HTTPS 传输 Token
5. **定期轮换**: 定期更换签名密钥

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 License

MIT License

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

## ⚠️ 注意事项

1. **密钥安全**: 生产环境务必使用强随机密钥，不要硬编码
2. **Token 时效**: Access Token 建议 15 分钟，Refresh Token 建议 1-7 天
3. **Redis 可用性**: 确保 Redis 高可用，建议使用主从或集群
4. **Claims 设计**: 不要在 Claims 中存储敏感信息（如密码）
5. **JTI 唯一性**: 系统自动生成 UUID 作为 JTI，确保全局唯一
6. **设备数限制**: 根据业务需求合理设置，防止恶意登录

## 📦 依赖

```bash
go get github.com/golang-jwt/jwt/v5
go get github.com/redis/go-redis/v9
go get github.com/kochabx/kit/core/tag  # 配置默认值支持
```

## 🧪 测试

```bash
# 单元测试
go test ./core/auth/jwt/...

# 性能测试
go test -bench=. ./core/auth/jwt

# 覆盖率
go test -cover ./core/auth/jwt/...