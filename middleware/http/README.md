# HTTP Middleware

HTTP 中间件集合，基于 Gin 框架。

## Auth 认证中间件

泛型认证中间件，支持 JWT、API Key 等多种认证方式。

### 快速开始

```go
// 1. 定义 Claims 结构
type UserClaims struct {
    jwt.RegisteredClaims
    UserID int64    `json:"uid"`
    Roles  []string `json:"roles"`
}

func (c *UserClaims) GetSubject() string {
    return c.Subject
}

// 2. 创建认证器
authenticator := middleware.AuthenticatorFunc[*UserClaims](func(ctx context.Context, token string) (*UserClaims, error) {
    claims := new(UserClaims)
    if err := jwtAuth.Verify(ctx, token, claims); err != nil {
        return nil, err
    }
    return claims, nil
})

// 3. 使用中间件
r.Use(middleware.Auth(middleware.AuthConfig[*UserClaims]{
    Authenticator: authenticator,
    SkipPaths:     []string{"/health", "/login"},
}))

// 4. Handler 中获取用户信息
func MyHandler(c *gin.Context) {
    claims, ok := middleware.GetClaims[*UserClaims](c.Request.Context())
    if ok {
        fmt.Println(claims.UserID, claims.Roles)
    }
}
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Authenticator` | `Authenticator[T]` | - | 认证器（必需） |
| `Extractor` | `TokenExtractor` | `BearerExtractor()` | Token 提取器 |
| `SkipPaths` | `[]string` | `[]` | 跳过认证的路径前缀 |
| `SkipFunc` | `func(*gin.Context) bool` | `nil` | 动态跳过判断函数 |
| `ContextKey` | `string` | `"claims"` | 上下文存储键 |
| `SuccessHandler` | `func(*gin.Context, T)` | `nil` | 认证成功回调 |
| `ErrorHandler` | `func(*gin.Context, error)` | 返回 401 | 错误处理函数 |

### Token 提取器

内置多种 Token 提取方式：

```go
// Bearer Token（默认）
// Authorization: Bearer <token>
BearerExtractor()

// 自定义 Header
// X-API-Key: <token>
HeaderExtractor("X-API-Key")

// URL Query 参数
// ?token=<token>
QueryExtractor("token")

// Cookie
CookieExtractor("session")

// 链式提取（依次尝试，第一个成功即返回）
ChainExtractor(
    BearerExtractor(),
    CookieExtractor("token"),
    QueryExtractor("access_token"),
)
```

### 跳过认证

```go
// 方式1: 路径前缀匹配
middleware.Auth(middleware.AuthConfig[*UserClaims]{
    Authenticator: auth,
    SkipPaths:     []string{"/health", "/api/v1/public"},
})

// 方式2: 动态判断
middleware.Auth(middleware.AuthConfig[*UserClaims]{
    Authenticator: auth,
    SkipFunc: func(c *gin.Context) bool {
        return c.GetHeader("X-Skip-Auth") == "true"
    },
})
```

### 自定义错误处理

```go
middleware.Auth(middleware.AuthConfig[*UserClaims]{
    Authenticator: auth,
    ErrorHandler: func(c *gin.Context, err error) {
        c.JSON(401, gin.H{
            "code":    "UNAUTHORIZED",
            "message": err.Error(),
        })
        c.Abort()
    },
})
```

### 错误类型

| 错误 | 说明 |
|------|------|
| `ErrTokenMissing` | Token 缺失 |
| `ErrTokenInvalid` | Token 格式无效 |
| `ErrAuthenticatorNil` | 认证器未配置 |
