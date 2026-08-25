# HMAC-SHA256 消息签名

该包用于对消息做短时效完整性和来源认证，支持随机 nonce、时间窗口和密钥轮换。
HMAC 不加密内容；需要机密性时应使用 `core/crypto/ecies`。

## 创建签名器

密钥必须至少 32 字节，并应来自安全随机源或密钥管理系统：

```go
signer, err := hmac.New(currentKey)
if err != nil {
    return err
}
```

默认使用 5 分钟有效期、`default` 密钥 ID 和零时钟偏差。需要调整时再传入：

```go
signer, err := hmac.New(currentKey,
    hmac.WithKeyID("2026-08"),
    hmac.WithExpiration(10*time.Minute),
    hmac.WithClockSkew(30*time.Second),
)
```

不要使用密码、短字符串或代码中的字面量作为 HMAC 密钥。可生成 256-bit
Base64 密钥，配置加载后先解码，再把得到的 32 字节传给 `New`：

```bash
openssl rand -base64 32
```

## 签名和验证

```go
signature, err := signer.Sign([]byte("需要认证的消息"))
if err != nil {
    return err
}

if err := signer.Verify(signature, []byte("需要认证的消息")); err != nil {
    return err
}
```

`Signature` 可以直接编码为 JSON：

```json
{
  "kid": "2026-08",
  "ts": 1787666400,
  "nonce": "Base64URL编码的随机值",
  "sig": "Base64URL编码的HMAC-SHA256"
}
```

签名覆盖格式版本、`kid`、时间戳、nonce 和 payload，每个字段都有明确边界。

## 防止重放

`Verify` 检查真实性和有效期，但不保存状态。业务必须在验证成功后，将
`signature.ReplayKey()` 原子写入共享存储，TTL 使用 `signer.Expiration()`：

```go
if err := signer.Verify(signature, payload); err != nil {
    return err
}

used, err := replayStore.Use(ctx, signature.ReplayKey(), signer.Expiration())
if err != nil {
    return err
}
if used {
    return ErrReplay
}
```

多副本服务应使用 Redis 的 `SET key value NX EX` 或等价原子操作。进程内 map
无法防止请求在不同实例之间重放。

## 密钥轮换

轮换期间同时配置新旧密钥，但只用新密钥签名：

```go
signer, err := hmac.New(newKey,
    hmac.WithKeyID("2026-09"),
    hmac.WithVerificationKey("2026-08", oldKey),
)
```

- 新签名的 `kid` 是 `2026-09`。
- 验证器仍能验证 `2026-08` 的未过期签名。
- 旧签名的最大有效期结束后再移除旧密钥。
- `kid` 是公开标识，不能包含密钥内容。

## 错误判断

```go
switch err := signer.Verify(signature, payload); {
case err == nil:
case errors.Is(err, hmac.ErrUnknownKey):
case errors.Is(err, hmac.ErrSignatureExpired):
case errors.Is(err, hmac.ErrFutureTimestamp):
case errors.Is(err, hmac.ErrSignatureMismatch):
case errors.Is(err, hmac.ErrInvalidSignature):
}
```

对外响应应统一为“签名无效”，不要暴露未知密钥、过期或摘要不匹配等细节。

## 安全约束

- 固定使用 HMAC-SHA256，不允许调用方选择弱哈希算法。
- 密钥长度至少 32 字节。
- 有效期必须为正数，不能关闭过期检查。
- nonce 使用 `crypto/rand` 生成 128 bit 随机值。
- 使用 `hmac.Equal` 做常量时间摘要比较。
- `Signer` 复制传入的密钥并支持并发使用。
- HMAC 使用共享密钥，签名方和验证方都能生成合法签名。
