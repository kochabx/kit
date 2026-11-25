# x/defaults

零第三方依赖的结构体默认值注入库，基于预编译反射计划、标准库解码与原子 Undo Log 回滚。

## 快速开始

```go
import "github.com/kochabx/kit/x/defaults"

type Config struct {
	Host    string        `json:"host" default:"127.0.0.1"`
	Port    int           `json:"port" default:"8080"`
	Timeout time.Duration `json:"timeout" default:"5s"`
	Tags    []string      `json:"tags" default:"[\"api\",\"v1\"]"`
}

func (c *Config) SetDefaults() {
	// 可选实现 Setter 接口，在静态 tag 填充完成后补充动态默认值
}

cfg, err := defaults.New[Config]()
// 或对已有结构体填充零值字段：
// err := defaults.Apply(&cfg)
```

只有**零值字段**会被写入默认值，已有显式赋值的字段保持不变；tag 值为 `"-"` 时跳过该字段。

## 支持类型

- **基础标量与具名别名**：`string`、`bool`、全部有符号/无符号整型（支持 `0x`/`0o`/`0` 进制，如 `fs.FileMode` 写 `default:"0644"`）、`float32`/`float64`、`complex64`/`complex128` 及任意层指针；
- **二进制切片**：`[]byte` 支持明文、`base64:...`、`hex:...` 前缀；
- **11 种标准库类型**：`time.Duration`、`time.Time`、`*time.Location`、`url.URL`、`url.Values`、`net.IP`、`net.IPNet`、`net.HardwareAddr`、`net.TCPAddr`、`net.UDPAddr`、`*regexp.Regexp`；
- **标准接口类型**：任意实现 `encoding.TextUnmarshaler` 或 `encoding/json/v2` `json.UnmarshalerFrom` 的类型（如 `netip.Addr`、`slog.Level` 等）；
- **复合类型**：`slice`、`map`、`array`、`struct`（通过严格 JSON 解码并递归填充嵌套子字段默认值）。

## 安全与选项

- **原子回滚**：任意字段解码失败时，通过 Undo Log 逆序回滚已修改的字段，且绝不复制/破坏 `sync.Mutex`、`*tls.Config` 等不可复制字段；
- **信息脱敏**：`FieldError` 故意不输出原始 tag 值，避免泄露默认密钥；
- **常用选项**：
  - `WithTag(tag)`：自定义 struct tag 名（默认 `"default"`）
  - `WithMaxDepth(n)`：限制嵌套遍历深度（默认 `64`）
  - `WithFieldFilter(fn)`：按字段元数据过滤跳过
  - `WithTimeLayouts(layouts...)`：自定义 `time.Time` 解析格式
  - `WithDecoder(typ, fn)`：为指定类型注册自定义解析函数

高频路径可先构造 `a, err := defaults.NewApplier(...)` 并复用 `a.Apply(&cfg)` / `a.New[T]()`。

## 验证

```bash
go test -race -count=1 ./x/defaults/...
```
