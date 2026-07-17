# log

基于 [zerolog](https://github.com/rs/zerolog) 的结构化日志库，提供文件轮转、多路输出和敏感数据脱敏能力。

## 特性

- 基于 zerolog，零分配 JSON 输出
- 支持按时间（rotatelogs）或按大小（lumberjack）滚动日志文件
- 支持控制台与文件同时输出
- 内置敏感数据脱敏，基于原子快照的无锁读取
- 提供全局日志实例，无需传递 logger
- 调用位置可选记录

## 快速开始

### 控制台日志

```go
import "github.com/kochabx/kit/log"

logger := log.New()

logger.Info().Msg("hello world")
logger.Debug().Str("key", "value").Msg("debug with field")
logger.Error().Err(err).Msg("something went wrong")
```

### 使用全局日志器

`init()` 时已自动初始化一个控制台全局日志器，可直接调用：

```go
log.Info().Msg("global info")
log.Info().Msgf("user %s logged in", username)
log.Error().Err(err).Msg("global error")
```

### 文件日志

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/writer"
)

config := writer.FileConfig{
    Path:         "logs/app.log",
    RotateMode: writer.RotateModeSize,
    SizeRotate: writer.SizeRotateConfig{
        MaxSize:    100,  // MB
        MaxBackups: 5,
        MaxAge:     30,   // 天
        Compress:   true,
    },
}

logger, err := log.NewFile(config)
if err != nil {
    panic(err)
}
defer logger.Close()

logger.Info().Msg("file log message")
```

### 同时输出到控制台和文件

```go
logger, err := log.NewMulti(config)
if err != nil {
    panic(err)
}
defer logger.Close()

logger.Info().Msg("multi output")
```

## 配置

### FileConfig

```go
type FileConfig struct {
    Path         string             // 完整日志路径，默认: "log/app.log"
    RotateMode writer.RotateMode
    TimeRotate TimeRotateConfig
    SizeRotate SizeRotateConfig
}
```

### 轮转模式

```go
writer.RotateModeTime  // 按时间轮转
writer.RotateModeSize  // 按大小轮转
```

### TimeRotateConfig（按时间轮转）

```go
type TimeRotateConfig struct {
    MaxAge   time.Duration // 日志保留时间，默认: 24h
    Interval time.Duration // 轮转间隔，默认: 1h
}
```

### SizeRotateConfig（按大小轮转）

```go
type SizeRotateConfig struct {
    MaxSize    int  // 单文件上限（MB），默认: 100
    MaxBackups int  // 保留旧文件数，默认: 5
    MaxAge     int  // 文件保留天数，默认: 30
    Compress   bool // 压缩旧文件，默认: false
}
```

## 选项

```go
log.WithLevel(zerolog.InfoLevel)  // 设置日志级别
log.WithCaller()                  // 记录调用位置
log.WithCallerSkip(skip int)      // 记录调用位置，跳过 skip 层封装
log.WithRedactor(redactor)         // 绑定脱敏 Redactor
```

## 数据脱敏

### 内置规则

```go
import "github.com/kochabx/kit/log/redact"

redactor, err := redact.New(redact.BuiltinRules()...)
if err != nil {
    panic(err)
}

logger := log.New(log.WithRedactor(redactor))
logger.Info().Str("phone", "13812345678").Msg("user info")
// 输出: "phone":"138****5678"
```

`BuiltinRules()` 默认完整隐藏 password、token 和 secret，手机号等标识类数据仅保留必要的首尾字符。

### 自定义规则

```go
redactor, err := redact.New(
    // 字段规则：精确匹配字段名并保留首尾字符
    redact.Field("id_no", redact.KeepEdges(6, 4)),
    // 内容规则：正则匹配后完整替换
    redact.Content("access-key", `AKIA[A-Z0-9]{16}`, redact.Replace("******")),
)
```

Redactor 支持运行时原子更新规则。执行计划保持不可变，日志读取路径无锁：

```go
redactor.AddRule(redact.Field("phone", redact.KeepEdges(3, 4)))
redactor.DisableRule("phone")
redactor.EnableRule("phone")
redactor.IsEnabled("phone")
redactor.RemoveRule("phone")
```

## 全局日志器

```go
// 替换全局日志器
logger, _ := log.NewFile(config, log.WithCaller(), log.WithLevel(zerolog.InfoLevel))
old := log.SetGlobal(logger)
defer old.Close()
```

全局函数返回 `*zerolog.Event`，支持链式调用：

```go
log.Debug().Str("k", "v").Msg("debug")
log.Info().Str("user", uid).Dur("elapsed", d).Msg("done")
log.Error().Err(err).Msg("failed")
log.Error().Stack().Err(err).Msg("failed with stack")
log.Fatal().Err(err).Msg("abort")   // 退出进程
```

## 最佳实践

### 生产环境

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/redact"
    "github.com/kochabx/kit/log/writer"
    "github.com/rs/zerolog"
)

redactor, err := redact.New(redact.BuiltinRules()...)
if err != nil {
    panic(err)
}

logger, err := log.NewFile(
    writer.FileConfig{
        RotateMode: writer.RotateModeSize,
        Path:         "/var/log/myapp/app.log",
        SizeRotate: writer.SizeRotateConfig{
            MaxSize:    100,
            MaxBackups: 10,
            MaxAge:     30,
            Compress:   true,
        },
    },
    log.WithCaller(),
    log.WithLevel(zerolog.InfoLevel),
    log.WithRedactor(redactor),
)
if err != nil {
    panic(err)
}
defer logger.Close()

old := log.SetGlobal(logger)
defer old.Close()
```

### 开发环境

```go
import "github.com/rs/zerolog"

logger := log.New(
    log.WithCaller(),
    log.WithLevel(zerolog.DebugLevel),
)
old := log.SetGlobal(logger)
defer old.Close()
```

### 结构化日志

```go
log.Info().
    Str("user_id", "12345").
    Int("status", 200).
    Dur("elapsed", time.Since(start)).
    Msg("request handled")
```

### 错误日志

```go
if err != nil {
    log.Error().
        Err(err).
        Str("op", "db.query").
        Str("table", "users").
        Msg("query failed")
}
```

## API 参考

### 创建日志器

| 函数 | 说明 |
|------|------|
| `New(opts ...Option) *Logger` | 控制台日志器 |
| `NewFile(c writer.FileConfig, opts ...Option) (*Logger, error)` | 文件日志器 |
| `NewMulti(c writer.FileConfig, opts ...Option) (*Logger, error)` | 文件 + 控制台双路输出 |

### Logger 方法

| 方法 | 说明 |
|------|------|
| `Close() error` | 释放文件句柄等资源 |
| `Redactor() *redact.Redactor` | 获取绑定的脱敏 Redactor |

### 全局函数

| 函数 | 说明 |
|------|------|
| `Global() *Logger` | 获取全局日志器 |
| `SetGlobal(logger *Logger) *Logger` | 原子替换并返回旧的全局日志器 |
| `ConfigureZerolog()` | 显式配置进程级时间和错误堆栈格式 |
| `Debug/Info/Warn/Error/Fatal/Panic() *zerolog.Event` | 创建对应级别的日志事件 |

### 脱敏 Redactor API

| 方法 | 说明 |
|------|------|
| `New(rules ...Rule) (*Redactor, error)` | 校验并编译初始规则 |
| `AddRule(rule Rule) error` | 原子添加并启用规则 |
| `AddRules(rules ...Rule) error` | 原子批量添加并启用规则 |
| `RemoveRule(name string) bool` | 原子删除规则 |
| `EnableRule(name string) bool` | 原子启用规则 |
| `DisableRule(name string) bool` | 原子禁用规则 |
| `IsEnabled(name string) bool` | 查询规则是否存在且已启用 |
| `Field(name string, mask Mask) Rule` | 创建字段规则 |
| `Content(name, pattern string, mask Mask) Rule` | 创建内容正则规则 |
| `Replace(value string) Mask` | 完整替换策略 |
| `KeepEdges(prefix, suffix int) Mask` | 保留首尾字符策略 |
| `Email() Mask` | 保留邮箱域名及用户名首尾字符 |
| `BuiltinRules() []Rule` | 获取内置安全规则 |
| `RedactString(value string) string` | 对字符串执行脱敏 |

## 模块结构

```
log/
├── logger.go              # Logger 实现
├── file.go                # 文件日志构造与配置
├── options.go             # Option 函数
├── global.go              # 全局日志实例与函数
├── zerolog.go             # Zerolog 进程级配置
├── writer/
│   ├── console.go         # 控制台 writer
│   ├── file.go            # 文件 writer 工厂
│   └── rotate.go          # RotateMode 与轮转实现
└── redact/
    ├── engine.go          # 动态规则管理与不可变执行计划
    ├── rule.go            # 字段规则与内容规则
    ├── mask.go            # 脱敏策略
    ├── writer.go          # 脱敏 io.Writer 包装
    └── builtin.go         # 内置规则
```

## 依赖

- [github.com/rs/zerolog](https://github.com/rs/zerolog)：核心日志库
- [github.com/lestrrat-go/file-rotatelogs](https://github.com/lestrrat-go/file-rotatelogs)：按时间轮转
- [gopkg.in/natefinch/lumberjack.v2](https://gopkg.in/natefinch/lumberjack.v2)：按大小轮转
