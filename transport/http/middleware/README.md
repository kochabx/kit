# HTTP Middleware

 HTTP 中间件集合，基于标准库 `net/http`，可无缝集成 Gin、Chi、Echo 等任意框架。

所有中间件均返回 `func(http.Handler) http.Handler`，通过以下方式组合：

```go
// 标准库
http.Handle("/", recovery(logger(corsMiddleware(myHandler))))

// Gin（通过 gin.WrapH 适配）
r.Use(gin.WrapH(Recovery()(http.NotFoundHandler())))
// 或直接包装 handler
r.GET("/path", func(c *gin.Context) {
    gin.WrapH(mw(inner)).ServeHTTP(c.Writer, c.Request)
})
```

## 中间件列表

| 中间件 | 函数 | 说明 |
|--------|------|------|
| 认证 | `Auth[T]()` | JWT / API Key 等多种认证方式 |
| CORS | `Cors()` | 跨域资源共享 |
| 加解密 | `Crypto()` | 请求体解密（ECIES / 自定义） |
| 日志 | `Logger()` | 请求日志，支持 Body / Header 记录 |
| 权限 | `Permission()` | 角色 / 所有权权限检查 |
| Recovery | `Recovery()` | Panic 恢复，返回 500 |
| 签名验证 | `Signature()` | 请求签名（HMAC-SHA256 / 自定义） |
| XSS 防护 | `Xss()` | Query / Form / JSON Body 过滤 |

---

## Auth 认证中间件

泛型认证中间件，支持 JWT、API Key 等多种认证方式。

### 快速开始

```go
// 1. 定义 Claims 结构（实现 jwt.Claims 接口）
type UserClaims struct {
    jwt.RegisteredClaims
    UserID int64    `json:"uid"`
    Roles  []string `json:"roles"`
}

func (c *UserClaims) GetSubject() string { return c.Subject }
func (c *UserClaims) GetRoles() []string { return c.Roles }

// 2. 创建认证器
auth := middleware.AuthenticatorFunc[*UserClaims](func(ctx context.Context, token string) (*UserClaims, error) {
    claims := new(UserClaims)
    if err := jwtAuth.Verify(ctx, token, claims); err != nil {
        return nil, err
    }
    return claims, nil
})

// 3. 注册中间件（标准库）
mux := http.NewServeMux()
authMw := middleware.Auth(middleware.AuthConfig[*UserClaims]{
    Authenticator: auth,
    SkipPaths:     []string{"/health", "/login"},
})
http.Handle("/", authMw(myHandler))

// 4. Handler 中获取用户信息
func myHandler(w http.ResponseWriter, r *http.Request) {
    claims, ok := middleware.GetClaims[*UserClaims](r.Context())
    if ok {
        fmt.Println(claims.UserID, claims.Roles)
    }
}
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Authenticator` | `Authenticator[T]` | — | 认证器（必需） |
| `Extractor` | `TokenExtractor` | `BearerExtractor()` | Token 提取器 |
| `ContextKey` | `string` | `"claims"` | 上下文存储键 |
| `SkipPaths` | `[]string` | `nil` | 跳过认证的路径，支持精确 / 前缀 `/**` / Glob |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |
| `SuccessHandler` | `func(http.ResponseWriter, *http.Request, T)` | `nil` | 认证成功回调 |
| `ErrorHandler` | `func(http.ResponseWriter, *http.Request, error)` | 返回 401 | 错误处理 |

### Token 提取器

```go
BearerExtractor()          // Authorization: Bearer <token>（默认）
HeaderExtractor("X-API-Key") // 任意 Header
QueryExtractor("token")    // URL Query 参数
CookieExtractor("session") // Cookie

// 链式提取：依次尝试，第一个成功即返回
ChainExtractor(
    BearerExtractor(),
    CookieExtractor("token"),
    QueryExtractor("access_token"),
)
```

### 错误变量

| 变量 | 说明 |
|------|------|
| `ErrTokenMissing` | Token 缺失（401） |
| `ErrTokenInvalid` | Token 格式无效（401） |
| `ErrAuthenticatorNil` | 认证器未配置（401） |

---

## CORS 中间件

```go
// 使用默认配置（允许所有来源）
http.Handle("/", middleware.Cors()(myHandler))

// 自定义配置
cors := middleware.Cors(middleware.CorsConfig{
    AllowOrigins:     []string{"https://example.com", "https://*.example.com"},
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders:     []string{"Authorization", "Content-Type"},
    AllowCredentials: true,
    MaxAge:           3600,
    SkipPaths:        []string{"/internal/**"},
})
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `AllowOrigins` | `[]string` | `["*"]` | 允许的来源，支持通配子域 `*.example.com` |
| `AllowMethods` | `[]string` | 常用方法 | 允许的 HTTP 方法 |
| `AllowHeaders` | `[]string` | 常用 Header | 允许的请求头 |
| `AllowCredentials` | `bool` | `false` | 是否允许携带凭证（与 `*` 来源互斥） |
| `ExposeHeaders` | `[]string` | `[]` | 暴露给客户端的响应头 |
| `MaxAge` | `int` | `43200` | 预检请求缓存时间（秒） |
| `SkipPaths` | `[]string` | `nil` | 跳过的路径 |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |

---

## Crypto 请求体解密中间件

对加密请求体进行 Base64 解码 + 解密，透明地替换 `r.Body`，后续 Handler 无感知。

```go
// 方式一：ECIES 私钥文件
decryptor, err := middleware.ECIESDecryptor("/path/to/private.pem")
// 或
decryptor := middleware.MustECIESDecryptor("/path/to/private.pem")

// 方式二：自定义解密器
decryptor := middleware.DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
    return myDecrypt(ciphertext)
})

mw := middleware.Crypto(middleware.CryptoConfig{
    Decryptor:    decryptor,
    Base64Decode: true, // 默认 true，先 Base64 解码再解密
    SkipPaths:    []string{"/health"},
})
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Decryptor` | `Decryptor` | — | 解密器（必需） |
| `Base64Decode` | `bool` | `true` | 解密前先进行 Base64 解码 |
| `SkipPaths` | `[]string` | `nil` | 跳过的路径 |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |
| `ErrorHandler` | `func(http.ResponseWriter, *http.Request, error)` | 返回 400 | 解密失败处理 |

---

## Logger 日志中间件

记录请求方法、路径、状态码、耗时、客户端 IP 等信息。

```go
mw := middleware.Logger(middleware.LoggerConfig{
    RequestBody:  true,                     // 记录请求体
    ResponseBody: true,                     // 记录响应体
    Header:       true,                     // 记录请求头
    SkipPaths:    []string{"/health", "/metrics"},
    SkipFunc: func(r *http.Request) bool {
        return r.Header.Get("X-Internal") == "true"
    },
})
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `RequestBody` | `bool` | `false` | 是否记录请求体 |
| `ResponseBody` | `bool` | `false` | 是否记录响应体 |
| `Header` | `bool` | `false` | 是否记录请求头 |
| `SkipPaths` | `[]string` | `nil` | 跳过记录的路径 |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |
| `Logger` | `*log.Logger` | 全局 Logger | 自定义 Logger |

客户端 IP 自动优先从 `X-Real-IP` → `X-Forwarded-For` → `RemoteAddr` 中提取。

---

## Permission 权限中间件

### 基础用法

```go
// 自定义权限检查器
checker := middleware.PermissionCheckerFunc(func(ctx context.Context, r *http.Request) error {
    claims, ok := middleware.GetClaims[*UserClaims](ctx)
    if !ok {
        return middleware.ErrUnauthorized
    }
    if claims.Role != "admin" {
        return middleware.ErrForbidden
    }
    return nil
})

mw := middleware.Permission(middleware.PermissionConfig{
    Checker:   checker,
    SkipPaths: []string{"/public/**"},
})
```

### 内置：基于角色（RoleBasedChecker）

```go
checker := middleware.RoleBasedChecker(middleware.RoleBasedConfig{
    AllowedRoles: []string{"admin", "editor"},
    // ClaimsKey 默认 "claims"，与 Auth 中间件一致
})

// Claims 需实现 GetRoles() []string，或通过 RolesGetter 自定义提取
checker2 := middleware.RoleBasedChecker(middleware.RoleBasedConfig{
    AllowedRoles: []string{"admin"},
    RolesGetter:  func(claims any) []string { return claims.(*UserClaims).Roles },
})
```

### 内置：基于所有权（OwnerBasedChecker）

```go
checker := middleware.OwnerBasedChecker(middleware.OwnerBasedConfig{
    SkipRoles: []string{"admin"},       // 管理员跳过所有权检查
    OwnerGetter: func(ctx context.Context, r *http.Request) ([]string, error) {
        id := chi.URLParam(r, "id")
        return db.GetResourceOwners(ctx, id)
    },
})
```

### 配置选项

**PermissionConfig**

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Checker` | `PermissionChecker` | — | 权限检查器（必需） |
| `SkipPaths` | `[]string` | `nil` | 跳过的路径 |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |
| `ErrorHandler` | `func(http.ResponseWriter, *http.Request, error)` | 透传错误码 | 错误处理 |

### 错误变量

| 变量 | 说明 |
|------|------|
| `ErrUnauthorized` | 未认证（401） |
| `ErrForbidden` | 无权限（403） |

---

## Recovery 中间件

捕获 Panic，自动返回 500；网络断开（broken pipe）时仅打印 Warn 日志，不写响应。

```go
mw := middleware.Recovery(middleware.RecoveryConfig{
    StackTrace: true, // 默认 true，在日志中记录堆栈信息
})
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `StackTrace` | `bool` | `true` | 是否在日志中记录堆栈 |
| `Logger` | `*log.Logger` | 全局 Logger | 自定义 Logger |

---

## Signature 请求签名验证中间件

验证请求签名，支持将 Query 参数、请求体、路径、方法组合签名。

```go
// HMAC-SHA256（内置）
signer := middleware.HMACSHA256Signer("my-secret")

mw := middleware.Signature(middleware.SignatureConfig{
    Signer:        signer,
    HeaderName:    "X-Signature", // 默认
    ParamsEnabled: true,          // 签名包含 Query 参数
    BodyEnabled:   true,          // 签名包含请求体
    PathEnabled:   false,         // 是否包含请求路径
    MethodEnabled: false,         // 是否包含请求方法
    SkipPaths:     []string{"/health"},
})
```

客户端签名示例（HMAC-SHA256 + Base64）：

```go
// 签名内容 = sorted(query_params) + body（按 ParamsEnabled/BodyEnabled 组合）
h := hmac.New(sha256.New, []byte("my-secret"))
h.Write(signingData)
signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
req.Header.Set("X-Signature", signature)
```

自定义签名验证器：

```go
signer := middleware.SignerFunc(func(data []byte, signature string) error {
    if !myVerify(data, signature) {
        return middleware.ErrSignatureFailed
    }
    return nil
})
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Signer` | `Signer` | — | 签名验证器（必需） |
| `HeaderName` | `string` | `"X-Signature"` | 签名 Header 名称 |
| `ParamsEnabled` | `bool` | `true` | 是否将 Query 参数纳入签名 |
| `BodyEnabled` | `bool` | `true` | 是否将请求体纳入签名 |
| `PathEnabled` | `bool` | `false` | 是否将请求路径纳入签名 |
| `MethodEnabled` | `bool` | `false` | 是否将请求方法纳入签名 |
| `SkipPaths` | `[]string` | `nil` | 跳过的路径 |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |
| `ErrorHandler` | `func(http.ResponseWriter, *http.Request, error)` | 返回 400 | 验证失败处理 |

---

## XSS 防护中间件

对请求中的 Query 参数、表单数据、JSON Body、Header 进行 XSS 过滤。默认使用 HTML 转义（`html.EscapeString`），过滤后数据对 Handler 透明。

```go
mw := middleware.Xss(middleware.XssConfig{
    QueryEnabled: true, // 过滤 Query 参数
    FormEnabled:  true, // 过滤表单 POST 数据
    BodyEnabled:  true, // 过滤 JSON Body（递归处理嵌套结构）
    SkipPaths:    []string{"/admin/**"},
    SkipFunc: func(r *http.Request) bool {
        return r.Header.Get("X-Trusted") == "1"
    },
})
```

自定义过滤器（如集成 bluemonday 等库）：

```go
mw := middleware.Xss(middleware.XssConfig{
    BodyEnabled: true,
    Sanitizer: middleware.SanitizerFunc(func(input string) string {
        return bluemonday.UGCPolicy().Sanitize(input)
    }),
})
```

### 配置选项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `QueryEnabled` | `bool` | `false` | 过滤 URL Query 参数 |
| `FormEnabled` | `bool` | `false` | 过滤表单数据（`r.Form` 和 `r.PostForm`） |
| `HeaderEnabled` | `bool` | `false` | 过滤请求 Header（慎用） |
| `BodyEnabled` | `bool` | `false` | 过滤 JSON Body，递归处理所有字符串字段 |
| `Sanitizer` | `Sanitizer` | `HTMLEscapeSanitizer()` | 自定义过滤器 |
| `SkipPaths` | `[]string` | `nil` | 跳过的路径 |
| `SkipFunc` | `func(*http.Request) bool` | `nil` | 动态跳过判断 |
| `SkipHeaders` | `[]string` | `nil` | 过滤 Header 时跳过的 Header 名称 |

---

## 路径匹配规则

所有 `SkipPaths` 均使用 `PathMatcher`，支持三种模式：

| 模式 | 示例 | 说明 |
|------|------|------|
| 精确匹配 | `/health` | 仅匹配 `/health` |
| 前缀匹配 | `/api/**` | 匹配 `/api` 及其所有子路径 |
| Glob 模式 | `/api/*/users` | 使用 `path.Match` 语法，`*` 不跨 `/` |

```go
SkipPaths: []string{
    "/health",          // 精确
    "/static/**",       // 前缀，匹配所有静态资源
    "/api/v*/internal", // Glob，匹配 /api/v1/internal、/api/v2/internal 等
}
```

