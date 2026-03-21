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
log.Infof("user %s logged in", username)
log.Error().Err(err).Msg("global error")
```

### 文件日志

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/writer"
)

config := log.FileConfig{
    RotateMode: writer.RotateModeSize,
    Filepath:   "logs",
    Filename:   "app",
    FileExt:    "log",
    LumberjackConfig: log.LumberjackConfig{
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
    Filepath         string            // 日志目录，默认: "log"
    Filename         string            // 文件名，默认: "app"
    FileExt          string            // 扩展名，默认: "log"
    RotateMode       writer.RotateMode // 轮转模式
    RotatelogsConfig RotatelogsConfig  // 按时间轮转配置
    LumberjackConfig LumberjackConfig  // 按大小轮转配置
}
```

### 轮转模式

```go
writer.RotateModeTime  // 按时间轮转（默认）
writer.RotateModeSize  // 按大小轮转
```

### RotatelogsConfig（按时间轮转）

```go
type RotatelogsConfig struct {
    MaxAge       int // 日志保留时间（小时），默认: 24
    RotationTime int // 轮转间隔（小时），默认: 1
}
```

### LumberjackConfig（按大小轮转）

```go
type LumberjackConfig struct {
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
log.WithDesensitize(hook)         // 绑定脱敏 Hook
```

## 数据脱敏

### 内置规则

```go
import "github.com/kochabx/kit/log/desensitize"

hook := desensitize.NewHook()

// 一次性添加常用内置规则（身份证、银行卡、password/token/secret 字段）
hook.AddBuiltin(desensitize.BuiltinRules()...)

// 手机号和邮箱单独添加
hook.AddBuiltin(desensitize.PhoneRule)  // 手机号: 138****5678
hook.AddBuiltin(desensitize.EmailRule)  // 邮箱: u***r@e***.com

logger := log.New(log.WithDesensitize(hook))
logger.Info().Str("phone", "13812345678").Msg("user info")
// 输出: "phone":"138****5678"
```

内置规则一览：

| 规则变量 | 类型 | 效果示例 |
|---------|------|---------|
| `PhoneRule` | ContentRule | `13812345678` → `138****5678` |
| `EmailRule` | ContentRule | `user@example.com` → `u***r@e***.com` |
| `IDCardRule` | ContentRule | `110101199001011234` → `110101********1234` |
| `BankCardRule` | ContentRule | `6222021234565678` → `6222 **** **** 5678` |
| `PasswordRule` | FieldRule | JSON `"password":"xxx"` → `"password":"******"` |
| `TokenRule` | FieldRule | JSON `"token":"xxx"` → `"token":"******"` |
| `SecretRule` | FieldRule | JSON `"secret":"xxx"` → `"secret":"******"` |

> `BuiltinRules()` 返回：IDCardRule、BankCardRule、PasswordRule、TokenRule、SecretRule。
> PhoneRule 和 EmailRule 可用，但不在 `BuiltinRules()` 中，需单独添加。

### 自定义规则

```go
hook := desensitize.NewHook()

// 内容规则：正则匹配文本中任意位置并替换
err := hook.AddContentRule("phone", `(1[3-9]\d)\d{4}(\d{4})`, "$1****$2")

// 字段规则：匹配 JSON 字段名后的值
err = hook.AddFieldRule("id_no", "id_no", `.*`, "********************")
```

### 规则管理

```go
hook.DisableRule("phone")   // 禁用，不删除规则
hook.EnableRule("phone")    // 重新启用
hook.IsEnabled("phone")     // 查询启用状态
hook.RemoveRule("phone")    // 永久删除
hook.GetRule("phone")       // 获取规则实例
hook.GetRules()             // 列出所有规则名
hook.RuleCount()            // 规则总数
hook.Clear()                // 清空
```

## 全局日志器

```go
// 替换全局日志器
logger, _ := log.NewFile(config, log.WithCaller(), log.WithLevel(zerolog.InfoLevel))
log.SetGlobalLogger(logger)

// 修改 zerolog 全局级别（影响所有 zerolog 实例）
log.SetZerologGlobalLevel(zerolog.WarnLevel)
```

全局函数返回 `*zerolog.Event`，支持链式调用：

```go
log.Debug().Str("k", "v").Msg("debug")
log.Info().Str("user", uid).Dur("elapsed", d).Msg("done")
log.Error().Err(err).Msg("failed")  // 自动附加堆栈
log.Fatal().Err(err).Msg("abort")   // 自动附加堆栈，退出进程
```

格式化函数（Error/Fatal/Panic 系列自动附加堆栈）：

```go
log.Infof("connected to %s", addr)
log.Errorf("query failed: %v", err)
```

## 最佳实践

### 生产环境

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/desensitize"
    "github.com/kochabx/kit/log/writer"
    "github.com/rs/zerolog"
)

hook := desensitize.NewHook()
hook.AddBuiltin(desensitize.BuiltinRules()...)
hook.AddBuiltin(desensitize.PhoneRule, desensitize.EmailRule)

logger, err := log.NewFile(
    log.FileConfig{
        RotateMode: writer.RotateModeSize,
        Filepath:   "/var/log/myapp",
        Filename:   "app",
        FileExt:    "log",
        LumberjackConfig: log.LumberjackConfig{
            MaxSize:    100,
            MaxBackups: 10,
            MaxAge:     30,
            Compress:   true,
        },
    },
    log.WithCaller(),
    log.WithLevel(zerolog.InfoLevel),
    log.WithDesensitize(hook),
)
if err != nil {
    panic(err)
}
defer logger.Close()

log.SetGlobalLogger(logger)
```

### 开发环境

```go
import "github.com/rs/zerolog"

logger := log.New(
    log.WithCaller(),
    log.WithLevel(zerolog.DebugLevel),
)
log.SetGlobalLogger(logger)
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
| `NewFile(c FileConfig, opts ...Option) (*Logger, error)` | 文件日志器 |
| `NewMulti(c FileConfig, opts ...Option) (*Logger, error)` | 文件 + 控制台双路输出 |

### Logger 方法

| 方法 | 说明 |
|------|------|
| `Close() error` | 释放文件句柄等资源 |
| `GetDesensitizeHook() *desensitize.Hook` | 获取绑定的脱敏 Hook |

### 全局函数

| 函数 | 说明 |
|------|------|
| `SetGlobalLogger(logger *Logger)` | 替换全局日志器 |
| `SetZerologGlobalLevel(level zerolog.Level)` | 设置 zerolog 全局级别 |
| `Debug/Info/Warn/Error/Fatal/Panic() *zerolog.Event` | 各级别（Error+ 自动附堆栈）|
| `Debugf/Infof/Warnf/Errorf/Fatalf/Panicf(...)` | 格式化输出（Error+ 自动附堆栈）|

### 脱敏 Hook 方法

| 方法 | 说明 |
|------|------|
| `NewHook() *Hook` | 创建 Hook |
| `AddRule(rule Rule)` | 添加规则实例 |
| `AddContentRule(name, pattern, replacement string) error` | 添加内容规则 |
| `AddFieldRule(name, fieldName, pattern, replacement string) error` | 添加字段规则 |
| `AddBuiltin(rules ...Rule)` | 批量添加规则 |
| `RemoveRule(name string) bool` | 删除规则 |
| `EnableRule(name string) bool` | 启用规则 |
| `DisableRule(name string) bool` | 禁用规则 |
| `IsEnabled(name string) bool` | 查询启用状态 |
| `GetRule(name string) (Rule, bool)` | 获取规则 |
| `GetRules() []string` | 列出所有规则名 |
| `RuleCount() int` | 规则数量 |
| `Clear()` | 清空所有规则 |

## 模块结构

```
log/
├── logger.go              # Logger 实现
├── config.go              # FileConfig / LumberjackConfig / RotatelogsConfig
├── options.go             # Option 函数
├── global.go              # 全局日志实例与函数
├── writer/
│   ├── console.go         # 控制台 writer
│   ├── file.go            # 文件 writer 工厂
│   └── rotate.go          # RotateMode 与轮转实现
└── desensitize/
    ├── desensitize.go     # Hook 与原子快照实现
    ├── rules.go           # Rule 接口、ContentRule、FieldRule
    ├── writer.go          # 脱敏 io.Writer 包装
    └── builtin.go         # 内置规则
```

## 依赖

- [github.com/rs/zerolog](https://github.com/rs/zerolog)：核心日志库
- [github.com/lestrrat-go/file-rotatelogs](https://github.com/lestrrat-go/file-rotatelogs)：按时间轮转
- [gopkg.in/natefinch/lumberjack.v2](https://gopkg.in/natefinch/lumberjack.v2)：按大小轮转
