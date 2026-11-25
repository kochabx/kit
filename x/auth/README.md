# x/auth

认证与多因子子模块合集：

- `github.com/kochabx/kit/x/auth/jwt` — 基于 `lestrrat-go/jwx/v3` 与 `uuid.NewV7()` 的 JWT 签发、验证、刷新、轮换与 JWKS（无 Redis 依赖）
- `github.com/kochabx/kit/x/auth/jwt/redis` — 基于 `redis.UniversalClient` 与 `EVALSHA` 的原子会话存储（按需引入）
- `github.com/kochabx/kit/x/auth/mfa` — 基于 `pquerna/otp` 的 TOTP/HOTP 校验与防重放存储
- `github.com/kochabx/kit/x/auth/mfa/qr` — 基于 `go-qrcode/v2` 的 PNG/SVG 二维码渲染

---

## 1. JWT 认证与会话轮换（`x/auth/jwt` + `x/auth/jwt/redis`）

```go
ring := jwt.NewKeyRing()
_ = ring.AddEd25519("k1", edPrivKey)

store, err := jwtredis.New(rdb.Client(), jwtredis.WithKeyPrefix("jwt:"))
if err != nil {
	return err
}

svc, err := jwt.New(
	jwt.Config{
		Issuer:                "auth.example.com",
		Audience:              []string{"api"},
		AccessTTL:             15 * time.Minute,
		RefreshTTL:            7 * 24 * time.Hour,
		MaxSessionsPerSubject: 5,
		KeyRing:               ring,
	},
	jwt.WithSessionStore(store),
	jwt.WithClaimsLoader(loadUserClaims), // 仅在签名与吊销校验通过后才调用
)
```

常用方法：`Issue`、`Verify(ctx, tok, jwt.TokenKindAccess)`、`Refresh`、`Revoke`、`RevokeDevice`、`RevokeAll`、`ListSessions`、`JWKS`。

---

## 2. 多因子认证与二维码（`x/auth/mfa` + `x/auth/mfa/qr`）

```go
totp, err := mfa.NewTOTP(mfa.TOTPConfig{Issuer: "ExampleApp"})
if err != nil {
	return err
}

enroll, err := totp.Generate("alice@example.com")
pngBytes, err := qr.EnrollmentPNG(enroll) // 或 qr.EnrollmentSVG(enroll)

if err := totp.Verify(ctx, "alice@example.com", enroll.Secret, code); err != nil {
	return err
}
```

支持传入自定义 `ReplayStore`（默认内置有界 `NewMemoryReplayStore`），另有 `NewHOTP` 支持基于计数器的 HOTP。

## 验证

```bash
go test -race -count=1 ./x/auth/...
```
