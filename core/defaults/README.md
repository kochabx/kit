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

无额外 Option 的 `Apply` 会复用默认 Applier 和解码配置。使用固定的自定义选项时，创建并共享 Applier：

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
- `url.URL`、`url.Values`（URL 查询字符串，支持重复参数）
- `net.IP`、`net.IPNet`、`net.HardwareAddr`（MAC 地址）
- `net.TCPAddr`、`net.UDPAddr`（IP:port，支持 IPv6 zone；不进行 DNS 查询）
- `netip.Addr`、`netip.AddrPort`、`netip.Prefix` 以及其他 `encoding.TextUnmarshaler`
- `math/big.Int`、`math/big.Rat`、`math/big.Float`（通过文本解码接口）
- `regexp.Regexp`
- `[]byte`，支持原文、`base64:` 和 `hex:` 前缀
- 指针
- Slice、Array、Map、Struct JSON
- `encoding/json/v2` 的 `Unmarshaler` 和 `UnmarshalerFrom`（后者优先）

复合类型必须使用 JSON，避免逗号、冒号和 URL 等内容产生歧义：

```go
type Config struct {
    Hosts  []string       `default:"[\"a:8080\",\"b:8080\"]"`
    Labels map[string]int `default:"{\"primary\":1}"`
}
```

JSON 结构体默认值会拒绝未知字段。整数按目标位宽解析，溢出会返回错误。

常用类型可以直接使用文本标签，指针同样适用：

```go
type Config struct {
    Listen *net.TCPAddr     `default:"[::1]:8080"`
    MAC    net.HardwareAddr `default:"02:00:5e:10:00:00"`
    Query  url.Values       `default:"name=a&name=b&path=%2Fapi"`
    Amount big.Int          `default:"12345678901234567890"`
    Ratio  big.Rat          `default:"22/7"`
}
```

集合与结构体 JSON 中，时长、时间、时区、URL、IP/CIDR、正则、MAC、TCP/UDP 地址和查询参数也支持字符串表示。时间字符串遵循 `WithTimeLayouts` 的设置：

```go
type Config struct {
    Delays []time.Duration `default:"[\"100ms\",\"2s\"]"`
    URLs   []url.URL       `default:"[\"https://example.com/api\"]"`
}
```

JSON 继续使用 v2 的严格语义（拒绝重复字段和无效 UTF-8、字段名区分大小写）。集合中的时长必须是带单位的字符串，不接受隐含单位的整数。其他类型的非字符串 JSON 表示交给标准 v2 解码器处理；自定义类型可实现文本或 JSON 解码接口。

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
applier, err := defaults.New(
    defaults.WithTag("fallback"),
    defaults.WithTimeLayouts(time.RFC3339, "2006-01-02 15:04:05"),
    defaults.WithFieldFilter(func(field reflect.StructField) bool {
        return field.Name != "Runtime"
    }),
)
if err != nil { return err }
err = applier.Apply(&config)
```

`WithDecoder(defaults.DecoderFunc(decode))` 可替换整个内置解码器；`decode` 的签名为 `func(reflect.Value, string) error`，此时常用类型和指针等解析也由调用方负责。所有 Option 都会在 `New` 时验证。

## 实现与使用边界

- `applier.go` 负责配置和事务提交，`traverse.go` 负责应用标签，`clone.go` 负责克隆。
- `decode.go` 处理基础类型，`decode_types.go` 集中注册常用类型，`decode_json.go` 适配 JSON v2。常用类型的文本与 JSON 字符串解码共用解析函数；每个 Applier 只构建一次 JSON 选项。
- `WithMaxDepth` 同时约束克隆和默认值遍历。深度按实际递归层级计算，包含指针、接口及集合；超限返回 `ErrMaxDepthExceeded`，不提交修改。
- 切片克隆区分长度和容量不同的视图。不同视图不保证保留共享底层数组的关系；仅克隆当前长度内的元素，不保留容量范围内隐藏的元素。
- 本模块面向配置数据。未导出字段按值复制，其中的指针、Map、Slice 可能仍引用原对象；不要传入锁、连接、文件句柄等运行时资源。自定义解码器不得通过未导出的共享状态或外部对象产生副作用，否则无法保证失败回滚。
- 同一个 Applier 可并发处理不同目标，但不要并发修改同一个目标。自定义 Decoder 和 FieldFilter 必须自行保证并发安全。

## 验证

```bash
go test -race ./core/defaults -count=1
```

测试覆盖常用类型、JSON v2 严格解码、切片视图、循环引用、深度限制、失败回滚和共享 Applier。类型和值的解码规则以本模块为准，约束校验交给 [validator](../validator/README.md)。
