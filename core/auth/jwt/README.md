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

```go
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
identity, err := auth.Authenticate(ctx, pair.AccessToken)
pair, err = auth.Refresh(ctx, pair.RefreshToken)

err = auth.RevokeSession(ctx, userID, sessionID)
err = auth.RevokeDevice(ctx, userID, deviceID)
err = auth.RevokeAll(ctx, userID)
sessions, err := auth.ListSessions(ctx, userID, jwt.SessionQuery{Limit: 20})
```

Refresh Token 单次使用。Redis Store 会原子比较并轮换 `jti`；重复使用旧 Refresh Token 会撤销对应会话并返回 `ErrRefreshReused`。
