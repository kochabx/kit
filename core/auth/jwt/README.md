# JWT 会话认证

`core/auth/jwt` 提供带 Redis 会话状态的 JWT 认证。Access Token 与 Refresh Token 使用不同的 audience；Redis Store 位于 `core/auth/jwt/store/redis`。

## Claims

JWT Payload 由标准 JWT Claims、框架管理的随机会话 ID（`sid`）和应用自定义字段组成。当前 Refresh JTI 保存在 Redis，不暴露在 Token 中。

自定义 Claims 需要匿名嵌入 `jwt.SessionClaims`：

```go
type UserClaims struct {
	jwt.SessionClaims
	UserID   string   `json:"user_id"`
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}
```

应用可以自由增加业务字段。`sid` 由模块签发，调用方不应赋值。

## 创建认证器

`jwt` 指 Kit 认证包，`gojwt` 指 `github.com/golang-jwt/jwt/v5`，`jwtredis` 指 `github.com/kochabx/kit/core/auth/jwt/store/redis`。下例 `redisClient` 是 `*github.com/kochabx/kit/store/redis.Client`，不是原生 go-redis 客户端。

```go
keyProvider, err := jwt.NewHMACKeyProvider("current", gojwt.SigningMethodHS256, signingSecret)
if err != nil {
    return err
}
store, err := jwtredis.New(redisClient)
if err != nil {
	return err
}

auth, err := jwt.New(jwt.AuthConfig[*UserClaims]{
	ClaimsFactory: func() *UserClaims { return new(UserClaims) },
	ClaimsLoader: func(ctx context.Context, subject string) (*UserClaims, error) {
		return loadUserClaims(ctx, subject)
	},
	KeyProvider:     keyProvider,
	Store:           store,
	Issuer:          "identity-service",
	AccessAudience:  []string{"api"},
	RefreshAudience: []string{"identity:refresh"},
	AccessTTL:       15 * time.Minute,
	RefreshTTL:      30 * 24 * time.Hour,
	MaxSessions:     10,
})
if err != nil {
    return err
}
```

`AccessAudience` 与 `RefreshAudience` 必须使用不同的值，避免两类 Token 被交叉接受。

## 使用

```go
claims := &UserClaims{
	SessionClaims: jwt.SessionClaims{
		RegisteredClaims: gojwt.RegisteredClaims{Subject: userID},
	},
	UserID: userID,
}

pair, err := auth.Issue(ctx, claims, jwt.WithIssueDeviceID(deviceID))
if err != nil { return err }
identity, err := auth.Authenticate(ctx, pair.AccessToken)
if err != nil { return err }
_ = identity
pair, err = auth.Refresh(ctx, pair.RefreshToken)
if err != nil { return err }

err = auth.RevokeSession(ctx, userID, sessionID)
err = auth.RevokeDevice(ctx, userID, deviceID)
err = auth.RevokeAll(ctx, userID)
sessions, err := auth.ListSessions(ctx, userID, jwt.SessionQuery{Limit: 20})
```

Refresh Token 单次使用。Redis Store 会原子比较并轮换 `jti`；重复使用旧 Refresh Token 会撤销对应会话并返回 `ErrRefreshReused`。

## 密钥、会话与 HTTP

HMAC secret 至少 32 字节；应用负责从密钥管理或运行环境加载。`NewKeySet` 支持区分签名密钥和验证密钥以及保留历史 key ID。Issuer、两组 Audience、TTL、ClaimsFactory、ClaimsLoader、Store 和 KeyProvider 都应按构造要求提供；RefreshTTL 必须大于 AccessTTL。

`Authenticate` 返回 `*jwt.Identity[T]`，不是直接返回 T。接入 HTTP Auth 中间件时，泛型类型需要与实际 Authenticate 返回值一致，或使用 AuthenticatorFunc 显式适配。会话查询按 `SessionPage.NextCursor` 分页；Session 包含内部刷新标识，不要将原始会话对象直接当作公开响应模型。

Redis Store 借用传入的客户端，不负责关闭它。认证过程依赖会话状态存储，不是纯离线 JWT 校验；业务应传递请求 Context，并对 ClaimsLoader 中的数据库/远程调用设置合理超时。

## 验证

```bash
go test -race ./core/auth/jwt/... -count=1
KIT_JWT_TEST_REDIS=localhost:6379 KIT_JWT_TEST_REDIS_PASSWORD=secret go test -race ./core/auth/jwt/store/redis -count=1
```

Redis Store 集成测试未设置地址时跳过；请使用测试专用 Redis。Claims 的克隆使用 `encoding/json/v2`，自定义字段应支持其编码语义。
