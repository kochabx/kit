# HTTP Middleware

中间件使用 `func(http.Handler) http.Handler`，可组合标准库 Handler，也可通过 `AdaptToGin` 接入 Gin。以下片段假定已导入本包及示例使用的标准库、Kit 模块。

## 组合与跳过规则

```go
handler := middleware.Recovery()(
    middleware.Logger()(
        middleware.Cors()(businessHandler),
    ),
)
```

最外层中间件最先处理请求。需要签名和解密时，按协议决定验签对象；通常先验证传输的密文请求，再解密并执行业务。

支持跳过的中间件统一使用 `SkipConfig`，不使用旧的 `SkipPaths` / `SkipFunc` 字段：

```go
skip := middleware.SkipConfig{
    Paths: []string{"/health", "/metrics", "/public/**"},
    Func: func(r *http.Request) bool { return r.Method == http.MethodOptions },
}
```

- `/health` 精确匹配。
- `/public/**` 匹配 `/public` 及其后代路径。
- `/api/*/users` 按 `path.Match` 匹配单层路径；`*` 不跨越 `/`。
- `Recovery` 没有 Skip 配置。

## 认证

```go
authenticator := middleware.AuthenticatorFunc[*UserClaims](
    func(ctx context.Context, token string) (*UserClaims, error) {
        return authenticateToken(ctx, token)
    },
)
authMiddleware := middleware.Auth(middleware.AuthConfig[*UserClaims]{
    Authenticator: authenticator,
    Skip: middleware.SkipConfig{Paths: []string{"/health", "/login"}},
})
handler := authMiddleware(businessHandler)
```

`Authenticator[T]` 只有 `Authenticate(context.Context, string) (T, error)`，不要求 Claims 实现特定接口。成功后结果存入 Context，默认键为 `"claims"`：

```go
claims, ok := middleware.GetClaims[*UserClaims](r.Context())
// 修改 AuthConfig.ContextKey 时，GetClaims 的可选 key 参数也应一致。
```

Token 默认从 `Authorization: Bearer ...` 提取，也可使用 `HeaderExtractor`、`CookieExtractor`、`QueryExtractor`，或 `ChainExtractor`。`ErrorHandler` 自定义失败响应；`SuccessHandler` 在成功后执行。JWT 会话认证的结果类型见 [JWT 文档](../../../core/auth/jwt/README.md)。

## CORS

```go
cfg := middleware.DefaultCorsConfig()
cfg.AllowOrigins = []string{"https://example.com"}
cfg.AllowCredentials = true
cfg.Skip = middleware.SkipConfig{Paths: []string{"/internal/**"}}
handler := middleware.Cors(cfg)(businessHandler)
```

`Cors()` 使用默认配置；传入配置时会替换整份配置，因此建议从 `DefaultCorsConfig()` 修改。`MaxAge` 单位为秒，`AllowMethods`、`AllowHeaders`、`ExposeHeaders` 控制响应头。凭证请求应使用明确的 Origin 配置。

## 请求体解密

```go
suite := ecies.X25519ChaCha20()
decryptor := middleware.HPKEDecryptor(suite, privateKey, []byte("app/request/v1"))
handler := middleware.Crypto(middleware.CryptoConfig{
    Decryptor: decryptor,
    Skip: middleware.SkipConfig{Paths: []string{"/health"}},
})(businessHandler)
```

默认 HPKE 解密器接收 JSON 格式的 `ecies.Message`，使用 `encoding/json/v2` 解码，解密后替换请求体。`info` 必须与发送方一致；该适配器不传 AAD，需要 AAD 时实现 `Decryptor` 或使用 `DecryptorFunc`。空请求体直接通过，Crypto 不加密响应。

```go
decryptor := middleware.DecryptorFunc(func(ciphertext []byte) ([]byte, error) {
    return decryptPayload(ciphertext)
})
```

输入按收到的字节原样交给解密器，不存在通用的额外 Base64 解码步骤。限制请求体大小应在外层设置。完整协议见 [HPKE 文档](../../../core/crypto/ecies/README.md)。

## 请求日志

```go
handler := middleware.Logger(middleware.LoggerConfig{
    Fields: middleware.LogFields{
        Trace: true,
        // 按实际需要开启 Header、RequestBody、ResponseBody。
    },
    Skip: middleware.SkipConfig{Paths: []string{"/health", "/metrics"}},
    Logger: logger,
})(businessHandler)
```

日志开关位于 `Fields`，不是 LoggerConfig 的直接字段。`Trace` 从当前 Context 中提取 trace/span ID，不会创建 OTel Provider 或 Span；需要链路关联时由应用在外层接入 tracing。`Enricher` 用于追加业务字段。记录请求头和正文前，应配置 [日志脱敏](../../../log/redact/README.md) 并控制数据量。

## 权限

权限中间件应位于认证之后，确保 Context 中已经存在身份：

```go
checker := middleware.RoleBasedChecker(middleware.RoleBasedConfig{
    AllowedRoles: []string{"admin", "editor"},
})
handler := middleware.Permission(middleware.PermissionConfig{
    Checker: checker,
    Skip: middleware.SkipConfig{Paths: []string{"/public/**"}},
})(businessHandler)
```

`RoleBasedChecker` 默认从 Claims 的 `GetRoles() []string` 读取角色，也可通过 `RolesGetter` 指定适配逻辑。`ClaimsKey` 需与认证配置一致。

`OwnerBasedChecker` 通过 `OwnerGetter(ctx, request)` 获取允许的主体 ID；支持 `SkipRoles`、`RolesGetter` 和 `OperatorGetter`。任意业务权限可用 `PermissionCheckerFunc` 实现。默认错误区分未认证与无权限，应用可自定义 `ErrorHandler`。

## Panic 恢复

```go
handler := middleware.Recovery()(businessHandler)
// 显式传入配置时会替换默认值：
handler = middleware.Recovery(middleware.RecoveryConfig{
    StackTrace: true,
    Logger: logger,
})(businessHandler)
```

无参数时默认记录堆栈。恢复逻辑只覆盖当前请求 goroutine；业务自行启动的 goroutine 需要自行管理错误和恢复。

## HTTP 签名与防重放

```go
signer, err := hmac.New(signingKey)
if err != nil {
    return err
}
if err := middleware.SignRequest(request, signer); err != nil {
    return err
}
handler := middleware.Signature(signer,
    middleware.WithSignatureReplayStore(replayStore),
    middleware.WithSignatureMaxBodyBytes(1<<20),
)(businessHandler)
```

签名覆盖请求方法、Host、转义路径、完整 Query、Content-Type 和正文摘要。默认 Header 为 `X-Signature`，正文限制为 1 MiB；`SignRequest` 保留正文可读性，签名完成后不要再修改被签名字段。

默认 ReplayStore 是进程内实现；多副本服务通过 `WithSignatureReplayStore` 注入共享的原子去重存储。还可设置 Header、Skip、成功/错误回调及 Logger。签名算法和密钥轮换见 [HMAC 文档](../../../core/crypto/hmac/README.md)。

## 框架接入

标准库直接包裹 Handler；Chi、Gorilla 等兼容 `net/http` 的路由器可使用其原生中间件接口。Gin 使用内置适配器：

```go
r := gin.New()
r.Use(middleware.AdaptToGin(middleware.Recovery()))
r.Use(middleware.AdaptToGin(middleware.Logger()))
r.Use(middleware.AdaptToGin(authMiddleware))
```

适配器会传递更新后的 Request，并在中间件拒绝请求时终止 Gin 调用链。其他框架需要使用各自支持的适配机制，本项目不提供通用 Fiber/Echo 适配器。

## 验证

```bash
go test -race ./transport/http/middleware -count=1
```

测试覆盖认证、跳过路径、请求日志、签名、权限、解密和框架适配。部分配置会记录请求信息，运行集成测试时使用测试专用凭证。
