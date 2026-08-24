# defaults

`defaults` 使用结构体标签为零值字段应用默认值。每次 Apply 都在深拷贝上执行；任意字段失败时，原目标保持不变。

```go
type Config struct {
    Host    string        `default:"localhost"`
    Port    uint16        `default:"8080"`
    Timeout time.Duration `default:"5s"`
}

var config Config
if err := defaults.Apply(&config); err != nil {
    return err
}
```

重复使用配置时创建并共享并发安全的 Applier：

```go
applier, err := defaults.New(
    defaults.WithTag("default"),
    defaults.WithMaxDepth(64),
)
err = applier.Apply(&config)
```

## 支持类型

- string、bool、所有位宽的 int/uint/float/complex
- `time.Duration`、`time.Time`、`time.Location`
- `url.URL`
- `net.IP`、`net.IPNet`
- `netip.Addr`、`netip.Prefix` 以及其他 `encoding.TextUnmarshaler`
- `regexp.Regexp`
- `[]byte`，支持原文、`base64:` 和 `hex:` 前缀
- 指针
- Slice、Array、Map、Struct JSON
- `json.Unmarshaler`

复合类型必须使用 JSON，避免逗号、冒号和 URL 等内容产生歧义：

```go
type Config struct {
    Hosts  []string       `default:"[\"a:8080\",\"b:8080\"]"`
    Labels map[string]int `default:"{\"primary\":1}"`
}
```

JSON 结构体默认值会拒绝未知字段。整数按目标位宽解析，溢出会返回错误。

## 语义

- 只修改零值字段；非零值不会被覆盖。
- `default:"-"` 跳过字段及其子树。
- `StructTag.Lookup` 用于区分无标签和显式空标签。
- nil 结构体指针仅在子类型包含默认标签时分配。
- 已存在的结构体指针、Slice、Array 和 Map 元素会递归应用默认值。
- 运行时对象环会被识别，不会依靠最大深度退出。
- 元数据缓存只分析单个类型，不递归等待，支持并发递归类型。
- 错误包含字段路径但不会打印原始标签值，以避免泄漏敏感默认值。

## 自定义

```go
defaults.New(
    defaults.WithTag("fallback"),
    defaults.WithTimeLayouts(time.RFC3339, "2006-01-02 15:04:05"),
    defaults.WithFieldFilter(func(field reflect.StructField) bool { ... }),
    defaults.WithDecoder(defaults.DecoderFunc(func(value reflect.Value, raw string) error { ... })),
)
```

`WithDecoder` 替换内置解码器。所有 Option 都会在 `New` 时验证。
