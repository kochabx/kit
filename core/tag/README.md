# Tag - Struct Tag Default Values

这个包提供了基于 struct tag 设置默认值的功能。

## 特性

- ✅ 支持所有基本类型（string, int, float, bool 等）
- ✅ 支持 `time.Duration` 类型（如 `1h30m`、`5s`）
- ✅ 支持 `[]byte` 类型
- ✅ 支持嵌套 struct
- ✅ 支持 slice 和 map
- ✅ 支持指针字段
- ✅ 递归深度限制
- ✅ 自定义 tag 名称
- ✅ 自定义值分隔符
- ✅ 自定义解析器
- ✅ 字段过滤器
- ✅ 详细的错误信息（包含字段路径）
- ✅ 支持 `encoding.TextUnmarshaler` 接口

## 快速开始

### 基本用法

```go
package main

import (
    "fmt"
    "github.com/kochabx/kit/core/tag"
)

type Config struct {
    Host    string `default:"localhost"`
    Port    int    `default:"8080"`
    Enabled bool   `default:"true"`
}

func main() {
    cfg := &Config{}
    if err := tag.ApplyDefaults(cfg); err != nil {
        panic(err)
    }
    
    fmt.Printf("%+v\n", cfg)
    // Output: {Host:localhost Port:8080 Enabled:true}
}
```

### 嵌套 Struct

```go
type Database struct {
    Host string `default:"localhost"`
    Port int    `default:"5432"`
}

type Config struct {
    DB Database
}

cfg := &Config{}
tag.ApplyDefaults(cfg) // DB 字段会自动递归处理
```

### Slice 支持

```go
type Config struct {
    Tags   []string `default:"dev,test,prod"`
    Ports  []int    `default:"8080,8081,8082"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg)
// Tags: ["dev", "test", "prod"]
// Ports: [8080, 8081, 8082]
```

### Map 支持

```go
type Config struct {
    Labels map[string]string `default:"env:prod,region:us"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg)
// Labels: {"env": "prod", "region": "us"}
```

### 指针字段

```go
type Config struct {
    Timeout *int `default:"30"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg)
// Timeout 指针会被创建并设置为 30
```

### time.Duration 支持

```go
type Config struct {
    Timeout  time.Duration   `default:"30s"`
    Interval time.Duration   `default:"1h30m"`
    Delays   []time.Duration `default:"1s,5s,10s"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg)
// Timeout: 30s, Interval: 1h30m
// Delays: [1s, 5s, 10s]
```

## 高级用法

### 自定义 Tag 名称

```go
type Config struct {
    Name string `mydefault:"example"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg, tag.WithTagName("mydefault"))
```

### 自定义分隔符

```go
type Config struct {
    Values []int `default:"1|2|3"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg, tag.WithSeparator("|"))
```

### 递归深度限制

```go
cfg := &DeepNestedConfig{}
err := tag.ApplyDefaults(cfg, tag.WithMaxDepth(10))
if err == tag.ErrMaxDepthExceeded {
    // 处理超出最大深度的情况
}
```

### 字段过滤器

```go
type Config struct {
    Public  string `default:"public"`
    Private string `default:"private" skip:"true"`
}

cfg := &Config{}
filter := func(field reflect.StructField) bool {
    return field.Tag.Get("skip") != "true"
}
tag.ApplyDefaults(cfg, tag.WithFieldFilter(filter))
// Private 字段会被跳过
```

### 自定义解析器

```go
type MyParser struct{}

func (p *MyParser) Parse(value reflect.Value, str string) error {
    // 自定义解析逻辑
    return nil
}

cfg := &Config{}
tag.ApplyDefaults(cfg, tag.WithParser(&MyParser{}))
```

### TextUnmarshaler 支持

实现 `encoding.TextUnmarshaler` 接口的类型会自动使用该接口进行解析：

```go
type MyTime time.Time

func (t *MyTime) UnmarshalText(text []byte) error {
    parsed, err := time.Parse("2006-01-02", string(text))
    if err != nil {
        return err
    }
    *t = MyTime(parsed)
    return nil
}

type Config struct {
    CreatedAt MyTime `default:"2024-01-01"`
}

cfg := &Config{}
tag.ApplyDefaults(cfg)
```

## 配置选项

| 选项 | 描述 | 默认值 |
|------|------|--------|
| `WithTagName(name string)` | 设置 tag 名称 | `"default"` |
| `WithMaxDepth(depth int)` | 设置最大递归深度 | `32` |
| `WithSeparator(sep string)` | 设置 slice 值的分隔符 | `","` |
| `WithParser(parser ValueParser)` | 设置自定义解析器 | 默认解析器 |
| `WithFieldFilter(filter FieldFilter)` | 设置字段过滤器 | `nil` |

## 错误处理

包提供了详细的错误信息，包含字段路径：

```go
type FieldError struct {
    Path  string       // 字段路径，如 "Config.Database.Port"
    Kind  reflect.Kind // 字段类型
    Tag   string       // Tag 名称
    Value string       // Tag 值
    Err   error        // 原始错误
}
```

常见错误类型：

- `ErrTargetMustBePointer` - 目标不是指针
- `ErrTargetIsNil` - 目标是 nil
- `ErrUnsupportedType` - 不支持的类型
- `ErrMaxDepthExceeded` - 超出最大递归深度
- `ErrInvalidTagValue` - 无效的 tag 值

## 性能

基准测试结果（Go 1.21+）：

```
BenchmarkApplyDefaults-8                 853302    1317 ns/op    352 B/op    3 allocs/op
BenchmarkApplyDefaultsComplex-8          853302    1317 ns/op    352 B/op    3 allocs/op
```

## 支持的类型

- ✅ 基本类型：string, bool, int/uint (所有大小), float32/float64
- ✅ 时间类型：time.Duration（支持 `1h30m`、`5s` 等格式）
- ✅ 字节类型：[]byte
- ✅ 复合类型：struct, slice, map, pointer
- ✅ 实现 `encoding.TextUnmarshaler` 接口的自定义类型
- ❌ 不支持：channel, function, unsafe pointer

## API 参考

### 函数

```go
// ApplyDefaults 根据 struct tag 为字段设置默认值
// target 必须是指向 struct 的指针
func ApplyDefaults(target any, opts ...Option) error
```

### 接口

```go
// ValueParser 定义值解析接口
type ValueParser interface {
    Parse(value reflect.Value, str string) error
}

// FieldFilter 定义字段过滤函数类型
type FieldFilter func(field reflect.StructField) bool
```

## 注意事项

1. **只处理零值字段**：已经有值的字段不会被覆盖
2. **嵌套 struct 自动处理**：即使没有 tag，嵌套的 struct 也会被递归处理
3. **slice 中的 struct**：已存在的 slice 元素中的 struct 会被递归处理
4. **map 格式**：使用 `key:value` 格式，多个键值对用分隔符分隔
5. **指针字段**：会自动创建新实例并设置值
