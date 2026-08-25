# 数据库字段加密

`envelope` 用于加密数据库中的密码、Access Token、API Key、第三方平台凭证等
小型敏感数据。它使用 AES-256-GCM，同时提供机密性、完整性和篡改检测。

## 最简单的使用方式

```go
cipher, err := envelope.New(masterKey)
if err != nil {
    return err
}

encrypted, err := cipher.Encrypt([]byte("third-party-access-token"))
if err != nil {
    return err
}

// encrypted 是 Base64URL 字符串，可以直接保存到数据库 TEXT 字段。

plaintext, err := cipher.Decrypt(encrypted)
if err != nil {
    return err
}
```

每次加密都会生成新的随机 nonce，因此相同明文重复加密会得到不同密文。

## 主密钥

主密钥必须恰好为 32 字节。生产环境应从 KMS、Vault、Secret Manager 或容器
Secret 中加载，不能保存在数据库、配置文件、日志或代码仓库里。

本地开发可以生成 Base64 格式密钥：

```bash
openssl rand -base64 32
```

加载配置后先解码：

```go
masterKey, err := base64.StdEncoding.DecodeString(config.MasterKey)
if err != nil {
    return err
}
defer clear(masterKey)

cipher, err := envelope.New(masterKey)
```

不要直接使用字符串密码作为 AES 密钥。如果来源只能是用户密码，应先使用
Argon2id 或其他专用 KDF 派生密钥。

## 加密结构化凭证

先把凭证编码成 JSON，再整体加密：

```go
type Credential struct {
    Username     string `json:"username"`
    Password     string `json:"password"`
    ClientSecret string `json:"client_secret"`
}

raw, err := json.Marshal(Credential{
    Username:     "service-user",
    Password:     "password",
    ClientSecret: "client-secret",
})
if err != nil {
    return err
}
defer clear(raw)

encrypted, err := cipher.Encrypt(raw)
if err != nil {
    return err
}

record.EncryptedCredential = encrypted
```

从数据库读取后：

```go
raw, err := cipher.Decrypt(record.EncryptedCredential)
if err != nil {
    return err
}
defer clear(raw)

var credential Credential
if err := json.Unmarshal(raw, &credential); err != nil {
    return err
}
```

## 使用 AAD 绑定数据库记录

建议使用 AAD 将密文绑定到租户、表和记录主键，防止攻击者把一条记录的密文
复制到另一条记录继续使用。AAD 被认证但不会被加密，也不会存入密文：

```go
aad := []byte("tenant=1001;table=integrations;id=42")

encrypted, err := cipher.EncryptWithAAD(rawCredential, aad)
plaintext, err := cipher.DecryptWithAAD(encrypted, aad)
```

AAD 必须可以在解密时可靠重建，并且字节完全一致。数据库主键在插入前未知时，
可以先生成 UUID 作为主键，再使用 UUID 构造 AAD。

不要把可能变化的字段放进 AAD，例如显示名称或更新时间；字段变化后旧密文将
无法解密。

## GORM 示例

```go
type Integration struct {
    ID                  string `gorm:"primaryKey"`
    TenantID            string `gorm:"index;not null"`
    EncryptedCredential string `gorm:"type:text;not null"`
}

func credentialAAD(record *Integration) []byte {
    return []byte("tenant=" + record.TenantID + ";table=integrations;id=" + record.ID)
}

func (service *Service) SaveCredential(ctx context.Context, record *Integration, raw []byte) error {
    encrypted, err := service.cipher.EncryptWithAAD(raw, credentialAAD(record))
    if err != nil {
        return err
    }
    record.EncryptedCredential = encrypted
    return service.db.WithContext(ctx).Save(record).Error
}

func (service *Service) LoadCredential(_ context.Context, record *Integration) ([]byte, error) {
    return service.cipher.DecryptWithAAD(record.EncryptedCredential, credentialAAD(record))
}
```

日志中不能记录 `raw`、解密结果、主密钥或完整数据库记录。

## 密钥轮换

密文内部携带公开的 key ID。轮换时用新密钥加密，同时保留旧密钥用于解密：

```go
cipher, err := envelope.New(newKey,
    envelope.WithKeyID("2026-09"),
    envelope.WithDecryptionKey("2026-08", oldKey),
)
```

推荐采用“读取时迁移”或后台批量迁移：

1. 部署同时包含新旧密钥的版本。
2. 所有新写入自动使用 `2026-09`。
3. 读取旧密文后，用当前密钥重新加密并更新数据库。
4. 确认数据库已没有旧 key ID 的密文。
5. 再从配置中移除旧密钥。

不要先删除旧密钥，否则历史数据将永久无法解密。

## 密文格式

`Encrypt` 返回一个不带填充的 Base64URL 字符串，解码后的版本 1 格式为：

```text
magic | version | key-id-length | key-id | nonce | ciphertext-and-tag
 4 B  |   1 B   |      1 B      | 可变   | 12 B  | 可变
```

格式头和调用方 AAD 都纳入 GCM 认证。修改版本、key ID、nonce、密文或认证标签
都会导致解密失败。

## 错误处理

```go
plaintext, err := cipher.Decrypt(encrypted)
switch {
case err == nil:
case errors.Is(err, envelope.ErrInvalidCiphertext):
case errors.Is(err, envelope.ErrUnknownKey):
case errors.Is(err, envelope.ErrDecryptionFailed):
}
```

对外接口应统一返回“凭证不可用”，不要暴露使用了哪个 key ID 或认证失败细节。

## 安全限制

- 默认最大明文为 1 MiB，可通过 `WithMaxPlaintextSize` 调整。
- 该包面向小型字段，不适合大文件；大文件应使用分块认证加密。
- 密文可以公开存储，但主密钥必须与数据库分离。
- 删除主密钥等同于永久删除所有由它加密且尚未迁移的数据。
- 不要在解密失败后回退为明文读取。
- Go 无法保证垃圾回收内存中的秘密立即清零，高安全场景应由 KMS/HSM 执行
  数据密钥解封或加解密操作。
