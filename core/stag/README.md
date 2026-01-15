# STag - Struct Tag Default Values

这个包提供了基于 struct tag 设置默认值的功能。

## 特性

- ✅ 支持所有基本类型（string, int, float, bool等）
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
    "github.com/kochabx/kit/core/stag"
)

type Config struct {
    Host    string `default:"localhost"`
    Port    int    `default:"8080"`
    Enabled bool   `default:"true"`
}

func main() {
    cfg := &Config{}
    if err := stag.ApplyDefaults(cfg); err != nil {
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
stag.ApplyDefaults(cfg) // DB 字段会自动递归处理
```

### Slice 支持

```go
type Config struct {
    Tags   []string `default:"dev,test,prod"`
    Ports  []int    `default:"8080,8081,8082"`
}

cfg := &Config{}
stag.ApplyDefaults(cfg)
// Tags: ["dev", "test", "prod"]
// Ports: [8080, 8081, 8082]
```

### Map 支持

```go
type Config struct {
    Labels map[string]string `default:"env:prod,region:us"`
}

cfg := &Config{}
stag.ApplyDefaults(cfg)
// Labels: {"env": "prod", "region": "us"}
```

### 指针字段

```go
type Config struct {
    Timeout *int `default:"30"`
}

cfg := &Config{}
stag.ApplyDefaults(cfg)
// Timeout 指针会被创建并设置为 30
```

## 高级用法

### 自定义 Tag 名称

```go
type Config struct {
    Name string `mydefault:"example"`
}

cfg := &Config{}
stag.ApplyDefaults(cfg, stag.WithTagName("mydefault"))
```

### 自定义分隔符

```go
type Config struct {
    Values []int `default:"1|2|3"`
}

cfg := &Config{}
stag.ApplyDefaults(cfg, stag.WithSeparator("|"))
```

### 递归深度限制

```go
cfg := &DeepNestedConfig{}
err := stag.ApplyDefaults(cfg, stag.WithMaxDepth(10))
if err == stag.ErrMaxDepthExceeded {
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
stag.ApplyDefaults(cfg, stag.WithFieldFilter(filter))
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
stag.ApplyDefaults(cfg, stag.WithParser(&MyParser{}))
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
    Path  string        // 字段路径，如 "Config.Database.Port"
    Kind  stag.Kind     // 字段类型
    Tag   string        // Tag 名称
    Value string        // Tag 值
    Err   error         // 原始错误
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
- ✅ 复合类型：struct, slice, map, pointer
- ✅ 实现 `encoding.TextUnmarshaler` 接口的自定义类型
- ❌ 不支持：channel, function, unsafe pointer
