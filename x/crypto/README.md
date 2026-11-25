# x/crypto

基于 Go 标准库的纯原生密码学门面，按功能拆分为三个无互相耦合的子包：

- `github.com/kochabx/kit/x/crypto/hpke` — RFC 9180 混合公钥加密与无停机密钥轮换
- `github.com/kochabx/kit/x/crypto/aead` — 每消息独立 HKDF 派生的 AES-256-GCM 认证加密
- `github.com/kochabx/kit/x/crypto/sign` — 防重放短期 HMAC 签名、Ed25519 与后量子 ML-DSA（FIPS 204）

---

## 1. `x/crypto/hpke`（非对称封装与轮换）

支持经典套件 `SuiteClassicX25519()` 与后量子混合套件 `SuiteHybridPQ()`（`MLKEM768X25519` + `SHAKE256` + `AES256GCM`）：

```go
priv, err := hpke.GenerateKeyPair(hpke.SuiteHybridPQ(), "k1")
if err != nil {
	return err
}
kr, err := hpke.NewKeyring(priv)
if err != nil {
	return err
}

env, err := kr.Seal([]byte("secret"), []byte("aad"))
plain, err := kr.Open(env, []byte("aad"))
```

支持 `AddPrivateKey` / `AddPublicKey` / `SetPrimary`、`KeyID(envelope)` 与 `NeedsReencrypt(envelope)`。

---

## 2. `x/crypto/aead`（AES-256-GCM 密文信封）

每条消息均随机生成盐并通过 HKDF 派生独立 AES-256-GCM 密钥，支持旧密钥平滑解密与重加密：

```go
c, err := aead.New(primaryKey,
	aead.WithKeyID("v2"),
	aead.WithDecryptionKey("v1", oldKey),
	aead.WithMaxPlaintext(1<<20),
)
if err != nil {
	return err
}

token, err := c.EncryptString([]byte("payload"), []byte("scope"))
plain, err := c.DecryptString(token, []byte("scope"))
```

---

## 3. `x/crypto/sign`（HMAC / Ed25519 / ML-DSA）

```go
// 短有效期防重放 HMAC-SHA256
signer, err := sign.NewHMAC(hmacKey,
	sign.WithHMACKeyID("k1"),
	sign.WithExpiration(5*time.Minute),
)
sig, err := signer.Sign(body)
err = signer.Verify(sig, body)
// ReplayTTL() = Expiration + Skew + 1s，可直接作为 Redis 去重 TTL

// Ed25519 与 ML-DSA
pub, priv, err := sign.GenerateEd25519()
sigBytes, err := sign.SignEd25519(priv, msg)
err = sign.VerifyEd25519(pub, msg, sigBytes)

pqPriv, err := sign.GenerateMLDSA(sign.MLDSA44())
pqSig, err := sign.SignMLDSA(pqPriv, msg, "ctx")
err = sign.VerifyMLDSA(pqPriv.PublicKey(), msg, pqSig, "ctx")
```

## 验证

```bash
go test -race -count=1 ./x/crypto/...
```
