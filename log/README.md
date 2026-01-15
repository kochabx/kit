# Log 日志模块

基于 [zerolog](https://github.com/rs/zerolog) 的高性能日志库，提供了结构化日志记录、日志轮转、数据脱敏等功能。

## 特性

- 🚀 **高性能**: 基于 zerolog，零分配的 JSON 日志记录
- 🔄 **日志轮转**: 支持按时间和大小进行日志轮转
- 🔒 **数据脱敏**: 内置敏感数据脱敏功能，保护隐私信息
- 📝 **多种输出**: 支持控制台、文件、多路输出
- 🎯 **结构化日志**: 支持结构化字段记录
- 📊 **调用栈**: 可选的调用栈信息记录
- 🌐 **全局日志**: 提供全局日志实例，方便使用
- 🧩 **模块化设计**: writer、desensitize 模块独立，易扩展

## 快速开始

### 基本使用

```go
package main

import (
    "github.com/kochabx/kit/log"
)

func main() {
    // 使用默认控制台日志
    logger := log.New()
    
    logger.Info().Msg("Hello, World!")
    logger.Debug().Str("key", "value").Msg("Debug with field")
    logger.Error().Err(err).Msg("Error occurred")
    
    // 使用全局日志（返回 *zerolog.Event，支持链式调用）
    log.Info().Msg("Global info log")
    log.Error().Err(err).Msg("Global error log")
    
    // 格式化便捷函数
    log.Infof("User %s logged in", username)
    log.Errorf("Failed to connect: %v", err)
}
```

### 文件日志

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/writer"
)

config := log.Config{
    RotateMode: writer.RotateModeSize,
    Filepath:   "logs",
    Filename:   "app",
    FileExt:    "log",
    LumberjackConfig: log.LumberjackConfig{
        MaxSize:    100,  // MB
        MaxBackups: 5,
        MaxAge:     30,   // days
        Compress:   true,
    },
}

logger, err := log.NewFile(config)
if err != nil {
    panic(err)
}
defer logger.Close() // 记得关闭以释放资源

logger.Info().Msg("File log message")
```

### 同时输出到文件和控制台

```go
logger, err := log.NewMulti(config)
if err != nil {
    panic(err)
}
defer logger.Close()

logger.Info().Msg("Multi output log")
```

## 配置说明

### Config 结构体

```go
type Config struct {
    RotateMode       writer.RotateMode // 轮转模式
    Filepath         string            // 日志文件路径，默认: "log"
    Filename         string            // 日志文件名，默认: "app"
    FileExt          string            // 日志文件扩展名，默认: "log"
    RotatelogsConfig RotatelogsConfig  // 按时间轮转配置
    LumberjackConfig LumberjackConfig  // 按大小轮转配置
}
```

### 轮转模式

```go
import "github.com/kochabx/kit/log/writer"

// 按时间轮转
writer.RotateModeTime

// 按大小轮转
writer.RotateModeSize
```

### 按时间轮转配置

```go
type RotatelogsConfig struct {
    MaxAge       int // 日志保留时间(小时)，默认: 24
    RotationTime int // 轮转时间间隔(小时)，默认: 1
}
```

### 按大小轮转配置

```go
type LumberjackConfig struct {
    MaxSize    int  // 单个日志文件最大大小(MB)，默认: 100
    MaxBackups int  // 保留的旧日志文件数量，默认: 5
    MaxAge     int  // 日志文件保留天数，默认: 30
    Compress   bool // 是否压缩旧日志文件，默认: false
}
```

## 高级功能

### 调用栈信息

```go
logger := log.New(log.WithCaller())
logger.Info().Msg("Log with caller info")

// 或者指定跳过的帧数
logger := log.New(log.WithCallerSkip(1))
```

### 数据脱敏

#### 使用内置规则

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/desensitize"
)

// 创建脱敏钩子
hook := desensitize.NewHook()

// 添加所有内置规则（手机、邮箱、身份证、银行卡、密码等）
hook.AddBuiltin(desensitize.BuiltinRules()...)

// 或者添加特定的内置规则
hook.AddBuiltin(desensitize.PhoneRuleSimple, desensitize.EmailRuleSimple)

// 创建带脱敏功能的日志器
logger := log.New(log.WithDesensitize(hook))

logger.Info().Str("phone", "13812345678").Msg("User info")  
// 输出: "13812345678" -> "1****5678"
```

#### 自定义脱敏规则

```go
// 创建脱敏钩子
hook := desensitize.NewHook()

// 添加手机号脱敏规则（内容规则）
err := hook.AddContentRule("phone", `1[3-9]\d{9}`, "1****5678")
if err != nil {
    panic(err)
}

// 添加邮箱脱敏规则
err = hook.AddContentRule("email", 
    `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, 
    "***@***.com")
if err != nil {
    panic(err)
}

// 添加 JSON 字段脱敏规则（字段规则）
err = hook.AddFieldRule("password", "password", ".+", "******")
if err != nil {
    panic(err)
}

// 创建带脱敏功能的日志器
logger := log.New(log.WithDesensitize(hook))
```

#### 内置脱敏规则说明

```go
// 可用的内置规则：
desensitize.PhoneRuleSimple     // 手机号: 1****5678
desensitize.EmailRuleSimple     // 邮箱: ***@***.com
desensitize.IDCardRule          // 身份证: 保留前6后4
desensitize.BankCardRule        // 银行卡: 保留前4后4
desensitize.PasswordRule        // password 字段
desensitize.TokenRule           // token 字段
desensitize.SecretRule          // secret 字段
```

### 全局日志配置

```go
// 设置全局日志级别
log.SetGlobalLevel(zerolog.InfoLevel)

// 设置自定义的全局日志器
logger, _ := log.NewFile(config, log.WithCaller())
log.SetGlobalLogger(logger)
```

## 脱敏功能详解

### 内容规则 (ContentRule)

基于正则表达式匹配日志内容进行脱敏：

```go
import "github.com/kochabx/kit/log/desensitize"

hook := desensitize.NewHook()

// 手机号脱敏
hook.AddContentRule("phone", `1[3-9]\d{9}`, "1****5678")

// 身份证号脱敏
hook.AddContentRule("idcard", `\d{17}[\dXx]`, "******************")

// 银行卡号脱敏
hook.AddContentRule("bankcard", `\d{16,19}`, "**** **** **** ****")
```

### 字段规则 (FieldRule)

基于 JSON 字段名进行脱敏：

```go
hook := desensitize.NewHook()

// 密码字段脱敏
hook.AddFieldRule("password", "password", ".+", "******")

// 令牌字段脱敏
hook.AddFieldRule("token", "token", ".+", "******")

// 邮箱字段脱敏（保留部分信息）
hook.AddFieldRule("email", "email", `(.+)@(.+)`, "$1***@***.com")
```

### 规则管理

```go
hook := desensitize.NewHook()

// 添加规则
hook.AddContentRule("phone", `1[3-9]\d{9}`, "1****5678")

// 禁用规则
hook.DisableRule("phone")

// 启用规则
hook.EnableRule("phone")

// 移除规则
hook.RemoveRule("phone")

// 获取规则
rule, exists := hook.GetRule("phone")

// 列出所有规则
ruleNames := hook.GetRules()

// 清空所有规则
hook.Clear()
```

## 最佳实践

### 1. 生产环境配置

```go
import (
    "github.com/kochabx/kit/log"
    "github.com/kochabx/kit/log/desensitize"
    "github.com/kochabx/kit/log/writer"
    "github.com/rs/zerolog"
)

config := log.Config{
    RotateMode: writer.RotateModeSize,
    Filepath:   "/var/log/myapp",
    Filename:   "app",
    FileExt:    "log",
    LumberjackConfig: log.LumberjackConfig{
        MaxSize:    100,  // 100MB
        MaxBackups: 10,   // 保留 10 个备份文件
        MaxAge:     30,   // 保留 30 天
        Compress:   true, // 压缩旧日志
    },
}

// 生产环境建议使用文件日志并配置脱敏
hook := desensitize.NewHook()
hook.AddBuiltin(desensitize.BuiltinRules()...)

logger, err := log.NewFile(config, 
    log.WithCaller(),
    log.WithLevel(zerolog.InfoLevel),
    log.WithDesensitize(hook),
)
if err != nil {
    panic(err)
}

log.SetGlobalLogger(logger)
```

### 2. 开发环境配置

```go
import (
    "github.com/rs/zerolog"
)

// 开发环境使用控制台输出，便于调试
logger := log.New(
    log.WithCaller(),
    log.WithLevel(zerolog.DebugLevel),
)
log.SetGlobalLogger(logger)
```

### 3. 结构化日志记录

```go
log.Info().
    Str("user_id", "12345").
    Int("age", 25).
    Dur("elapsed", time.Since(start)).
    Msg("User operation completed")
```

### 4. 错误日志记录

```go
if err != nil {
    log.Error().
        Err(err).
        Str("operation", "database_query").
        Str("table", "users").
        Msg("Database operation failed")
    return err
}
```

### 5. 资源管理

```go
// 使用文件日志时记得关闭
logger, err := log.NewFile(config)
if err != nil {
    return err
}
defer logger.Close() // 释放文件句柄等资源
```

## API 参考

### 创建日志器

- `New(opts ...Option) *Logger` - 创建控制台日志器
- `NewFile(config Config, opts ...Option) (*Logger, error)` - 创建文件日志器
- `NewMulti(config Config, opts ...Option) (*Logger, error)` - 创建多路输出日志器

### 选项函数

- `WithLevel(level zerolog.Level)` - 设置日志级别
- `WithCaller()` - 添加调用栈信息
- `WithCallerSkip(skip int)` - 添加调用栈信息并跳过指定帧数
- `WithDesensitize(hook *desensitize.Hook)` - 添加脱敏功能

### 全局日志函数

返回 `*zerolog.Event`，支持链式调用：

- `Debug() *zerolog.Event` - Debug 级别日志
- `Info() *zerolog.Event` - Info 级别日志
- `Warn() *zerolog.Event` - Warn 级别日志
- `Error() *zerolog.Event` - Error 级别日志（带堆栈）
- `Fatal() *zerolog.Event` - Fatal 级别日志（带堆栈）
- `Panic() *zerolog.Event` - Panic 级别日志（带堆栈）

格式化便捷函数：

- `Debugf(format string, args ...any)` - 格式化 Debug 日志
- `Infof(format string, args ...any)` - 格式化 Info 日志
- `Warnf(format string, args ...any)` - 格式化 Warn 日志
- `Errorf(format string, args ...any)` - 格式化 Error 日志（带堆栈）
- `Fatalf(format string, args ...any)` - 格式化 Fatal 日志（带堆栈）
- `Panicf(format string, args ...any)` - 格式化 Panic 日志（带堆栈）

### 全局配置

- `SetGlobalLogger(logger *Logger)` - 设置全局日志实例
- `SetGlobalLevel(level zerolog.Level)` - 设置全局日志级别
- `SetZerologGlobalLevel(level zerolog.Level)` - 设置 zerolog 全局级别

### 脱敏模块 (desensitize 包)

**创建钩子：**

- `NewHook() *Hook` - 创建脱敏钩子

**添加规则：**

- `AddRule(rule Rule)` - 添加规则实例
- `AddContentRule(name, pattern, replacement string) error` - 添加内容规则
- `AddFieldRule(name, fieldName, pattern, replacement string) error` - 添加字段规则
- `AddBuiltin(rules ...Rule)` - 添加内置规则

**规则管理：**

- `RemoveRule(name string) bool` - 移除规则
- `EnableRule(name string) bool` - 启用规则
- `DisableRule(name string) bool` - 禁用规则
- `GetRule(name string) (Rule, bool)` - 获取规则
- `GetRules() []string` - 列出所有规则名称
- `RuleCount() int` - 获取规则数量
- `Clear()` - 清空所有规则

**内置规则：**

- `PhoneRuleSimple` - 手机号脱敏
- `EmailRuleSimple` - 邮箱脱敏
- `IDCardRule` - 身份证脱敏
- `BankCardRule` - 银行卡脱敏
- `PasswordRule` - password 字段脱敏
- `TokenRule` - token 字段脱敏
- `SecretRule` - secret 字段脱敏
- `BuiltinRules()` - 返回所有内置规则

## 模块结构

```
log/
├── logger.go              # 核心 Logger 实现
├── config.go              # 配置结构
├── options.go             # 选项函数
├── global.go              # 全局日志实例
├── writer/                # Writer 模块
│   ├── console.go         # 控制台输出
│   ├── file.go            # 文件输出
│   └── rotate.go          # 日志轮转
├── desensitize/           # 脱敏模块
│   ├── desensitize.go     # 核心实现
│   ├── rules.go           # 规则定义
│   ├── writer.go          # 脱敏 Writer
│   └── builtin.go         # 内置规则
└── internal/              # 内部工具
    └── pool.go            # 对象池
```

## 依赖

- [github.com/rs/zerolog](https://github.com/rs/zerolog) - 高性能日志库
- [github.com/lestrrat-go/file-rotatelogs](https://github.com/lestrrat-go/file-rotatelogs) - 按时间轮转
- [gopkg.in/natefinch/lumberjack.v2](https://gopkg.in/natefinch/lumberjack.v2) - 按大小轮转
