# HPKE 加密

`ecies` 提供基于 Go 标准库 `crypto/hpke` 和 RFC 9180 的混合公钥加密能力。

HPKE 的基本使用方式是：发送方持有接收方的公钥并调用 `Seal`，接收方持有
对应私钥并调用 `Open`。私钥不能发送给客户端或其他非受信任系统。

## 安装

```bash
go get github.com/kochabx/kit/core/crypto/ecies
```

需要 Go 1.26 或更高版本。

## 选择加密套件

一般业务使用 `X25519ChaCha20`：

```go
suite := ecies.X25519ChaCha20()
```

它使用：

- DHKEM(X25519, HKDF-SHA256)
- HKDF-SHA256
- ChaCha20-Poly1305

需要后量子保护且通信双方都支持时，可以使用：

```go
suite := ecies.HybridPostQuantum()
```

该套件组合 ML-KEM-768 与 X25519，密钥和消息会明显更大。加密、解密、
保存和加载密钥时必须始终使用同一个套件，两个套件的密钥不能混用。

## 公钥加密、私钥解密的完整流程

实际使用时，发送方不生成密钥，也不持有私钥。完整流程如下：

```text
接收方生成密钥对
    │
    ├── 私钥：只保存在接收方
    └── 公钥：发布给发送方
                    │
发送方加载公钥 ────┘
    │
    ├── 使用公钥加密
    └── 发送 Message 和必要的公开元数据
                    │
接收方使用私钥解密 ┘
```

### 第一步：接收方生成并发布公钥

接收方只需要生成一次密钥对。私钥保存在接收方，公钥可以通过 HTTPS 接口、
配置中心或其他可信渠道提供给发送方：

```go
suite := ecies.X25519ChaCha20()

privateKey, err := suite.GenerateKey()
if err != nil {
    return err
}

privateBytes, err := privateKey.Bytes()
if err != nil {
    return err
}
defer clear(privateBytes)

// 私钥仅由接收方保存，不能发送给别人。
if err := os.WriteFile("private.key", privateBytes, 0o600); err != nil {
    return err
}

// 公钥可以发布给所有需要向本服务发送密文的人。
publicBytes := privateKey.Public().Bytes()
if err := os.WriteFile("public.key", publicBytes, 0o644); err != nil {
    return err
}
```

### 第二步：发送方只使用公钥加密

发送方从接收方获得 `public.key`，不需要也不应该获得私钥：

```go
suite := ecies.X25519ChaCha20()

publicBytes, err := os.ReadFile("public.key")
if err != nil {
    return err
}

receiverPublicKey, err := suite.ParsePublicKey(publicBytes)
if err != nil {
    return err
}

info := []byte("example/order-message/v1")
aad := []byte("tenant=1001;order=202608250001")
plaintext := []byte("需要加密的数据")

// 这里只需要接收方的公钥。
message, err := suite.Seal(receiverPublicKey, info, aad, plaintext)
if err != nil {
    return err
}

// JSON 会自动将 enc 和 ct 编码为 Base64。
requestBody, err := json.Marshal(message)
if err != nil {
    return err
}

// 将 requestBody 发送给接收方。
```

### 第三步：接收方使用私钥解密

```go
suite := ecies.X25519ChaCha20()

privateBytes, err := os.ReadFile("private.key")
if err != nil {
    return err
}
defer clear(privateBytes)

privateKey, err := suite.ParsePrivateKey(privateBytes)
if err != nil {
    return err
}

// requestBody 是发送方传来的 JSON 数据。
var message ecies.Message
if err := json.Unmarshal(requestBody, &message); err != nil {
    return err
}

// info 和 aad 必须与发送方完全相同。
info := []byte("example/order-message/v1")
aad := []byte("tenant=1001;order=202608250001")

plaintext, err := suite.Open(privateKey, info, aad, &message)
if err != nil {
    return err
}

fmt.Println(string(plaintext))
```

`aad` 不包含在 `Message` 中。接收方应从可信的请求上下文重建它，例如 HTTP
路径、租户身份和订单号；如果需要在请求中传输 AAD 字段，必须将这些字段放在
`Message` 外面，并在解密前按双方约定的固定格式重新编码。

`Seal` 每次都会产生新的随机封装结果。同一个公钥和明文重复加密，得到的
`Message` 也不同。

## info 和 AAD 怎么填写

### info：协议域隔离

`info` 用于标识“这段密文属于哪个协议”，应当是稳定、唯一的常量，例如：

```go
var orderMessageInfo = []byte("kochabx/order-service/order-message/v1")
```

推荐包含组织、服务、用途和版本。不要使用时间戳或随机数，也不要在加密端
和解密端分别临时拼接。修改 `info` 后，旧消息将无法解密。

### AAD：绑定业务元数据

AAD 会被认证，但不会被加密，也不会自动存入 `Message`。适合绑定：

- 租户 ID
- 用户 ID
- 数据库记录 ID
- 消息类型
- HTTP 方法和路径

例如：

```go
aad := []byte("tenant=1001;type=order;id=202608250001")
```

攻击者只要修改 AAD、密文或封装密钥，`Open` 就会失败。AAD 的字段顺序和
编码必须固定；复杂结构建议先定义结构体，再使用确定性的编码规则。不要直接
依赖普通 map 的字段顺序。

不需要 AAD 时可以传 `nil`：

```go
message, err := suite.Seal(publicKey, info, nil, plaintext)
plaintext, err := suite.Open(privateKey, info, nil, message)
```

## Message 的 JSON 传输

`Message` 包含两个字段：

```go
type Message struct {
    Encapsulation []byte `json:"enc"`
    Ciphertext    []byte `json:"ct"`
}
```

Go 的 `encoding/json` 会自动把两个字节切片编码成 Base64，因此不需要再手动
Base64 编码：

```go
encoded, err := json.Marshal(message)
if err != nil {
    return err
}

// encoded 可以作为 HTTP Body、Kafka 消息或数据库字段传输。

var received ecies.Message
if err := json.Unmarshal(encoded, &received); err != nil {
    return err
}

plaintext, err := suite.Open(privateKey, info, aad, &received)
```

JSON 大致如下：

```json
{
  "enc": "Base64 编码的临时封装密钥",
  "ct": "Base64 编码的认证密文"
}
```

不要只保存 `ct`；解密同时需要 `enc`、正确的私钥、套件、`info` 和 AAD。

## 保存和加载密钥

当前包只提供 RFC 9180 密钥序列化，不负责决定密钥存储位置。生产环境优先
使用 KMS、Vault 或 Secret Manager。下面的文件示例适合本地开发或受控部署。

### 保存密钥

```go
privateBytes, err := privateKey.Bytes()
if err != nil {
    return err
}
defer clear(privateBytes)

if err := os.WriteFile("private.key", privateBytes, 0o600); err != nil {
    return err
}

publicBytes := privateKey.Public().Bytes()
if err := os.WriteFile("public.key", publicBytes, 0o644); err != nil {
    return err
}
```

如果文件已经存在，`os.WriteFile` 不会自动收紧原文件权限，应另外执行：

```go
if err := os.Chmod("private.key", 0o600); err != nil {
    return err
}
```

### 加载私钥

```go
suite := ecies.X25519ChaCha20()

privateBytes, err := os.ReadFile("private.key")
if err != nil {
    return err
}
defer clear(privateBytes)

privateKey, err := suite.ParsePrivateKey(privateBytes)
if err != nil {
    return err
}
```

### 加载公钥

```go
publicBytes, err := os.ReadFile("public.key")
if err != nil {
    return err
}

publicKey, err := suite.ParsePublicKey(publicBytes)
if err != nil {
    return err
}
```

密钥文件没有携带套件标识。保存密钥时应在配置或元数据中同时记录套件名称，
例如 `X25519ChaCha20`，加载时选择相同套件。

## HTTP 请求体加密

服务端可以使用 `transport/http/middleware.HPKEDecryptor` 解密 JSON 格式的
`ecies.Message`：

```go
suite := ecies.X25519ChaCha20()
privateKey, err := suite.ParsePrivateKey(privateKeyBytes)
if err != nil {
    return err
}

decryptor := middleware.HPKEDecryptor(
    suite,
    privateKey,
    []byte("kochabx/order-api/http-body/v1"),
)

handler := middleware.Crypto(middleware.CryptoConfig{
    Decryptor: decryptor,
    Skip: middleware.SkipConfig{
        Paths: []string{"/health"},
    },
})(apiHandler)
```

客户端需要使用相同的 `suite` 和 `info`：

```go
message, err := suite.Seal(
    serverPublicKey,
    []byte("kochabx/order-api/http-body/v1"),
    nil,
    requestJSON,
)
if err != nil {
    return err
}

requestBody, err := json.Marshal(message)
```

`HPKEDecryptor` 当前使用 `nil` AAD。如果需要把 HTTP 方法、路径、租户等信息
作为 AAD 绑定，应实现自定义请求级中间件，因为普通 `Decryptor` 接口拿不到
`*http.Request`。

## 错误判断

所有底层错误都会保留错误链，可以使用 `errors.Is`：

```go
plaintext, err := suite.Open(privateKey, info, aad, message)
switch {
case err == nil:
    // 解密成功
case errors.Is(err, ecies.ErrInvalidKey):
    // 私钥为空或套件不匹配
case errors.Is(err, ecies.ErrInvalidMessage):
    // enc 或 ct 缺失
case errors.Is(err, ecies.ErrDecryptionFailed):
    // 密钥、info、AAD 不匹配，或者消息被篡改
default:
    // 其他错误
}
```

对外不要暴露详细解密失败原因。统一返回“无效密文”可以避免泄漏密钥、协议和
认证状态信息。

## 并发使用

`Suite` 和密钥对象可以被多个 goroutine 读取使用。每次 `Seal` 和 `Open` 都会
创建独立的一次性 HPKE 上下文，因此不要自行缓存或跨请求共享 HPKE Sender、
Recipient 状态。

## 安全注意事项

- 私钥只能保存在服务端受保护的存储中，不能写入日志或提交到代码仓库。
- 一套密钥只用于一种明确用途，不要同时用于登录令牌、文件加密和接口加密。
- 始终使用固定且唯一的 `info`，并在协议升级时递增版本。
- 尽可能使用 AAD 绑定租户、记录和消息类型，防止密文被挪到其他上下文使用。
- 不要修改、截断或只保存 `Message` 的一个字段。
- 不要因为 `Open` 失败而回退到明文处理。
- HPKE 适合加密消息和数据密钥，不适合一次性加载并加密超大文件；大文件应使用
  分块认证加密或信封加密方案。
- Go 无法保证垃圾回收内存中的私钥被立即清零；真正高安全等级的密钥应由 KMS、
  HSM 或独立密钥服务持有。

## API 一览

| API | 用途 |
|---|---|
| `X25519ChaCha20()` | 创建通用 HPKE 套件 |
| `HybridPostQuantum()` | 创建 ML-KEM-768 + X25519 混合套件 |
| `Suite.GenerateKey()` | 生成私钥和对应公钥 |
| `PrivateKey.Public()` | 获取公钥 |
| `PrivateKey.Bytes()` | 序列化私钥 |
| `PublicKey.Bytes()` | 序列化公钥 |
| `Suite.ParsePrivateKey()` | 解析私钥 |
| `Suite.ParsePublicKey()` | 解析公钥 |
| `Suite.Seal()` | 使用公钥加密 |
| `Suite.Open()` | 使用私钥认证并解密 |
