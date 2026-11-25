# log

基于 Go 标准库 **`log/slog`** 的结构化日志包，整合了 [`lmittmann/tint`](https://github.com/lmittmann/tint) 彩色控制台输出、[`timberjack`](https://github.com/DeRuina/timberjack) 大小/时间双模式轮转、无锁递归脱敏 [`Redactor`](file:///root/workspace/kit/log/redact.go) 与 Context 属性提取，并提供开箱即用的包级全局日志入口。

## 快速开始

### 全局日志（导入即用）

包初始化时已自动构建默认彩色控制台日志器（`slog.LevelInfo`、`os.Stdout`），可直接调用包级函数：

```go
import (
	"context"
	"log/slog"

	"github.com/kochabx/kit/log"
)

func handle(ctx context.Context, err error) {
	log.Info("service started", slog.String("addr", ":8080"))
	log.Warn("cache miss", slog.String("key", "session:1"))
	log.ErrorContext(ctx, "request failed", slog.Any("err", err))
}
```

### 替换全局日志器

可通过 `log.SetDefault` 在服务启动阶段将全局日志器替换为带 caller、脱敏规则、链路提取或文件轮转的实例：

```go
logger, err := log.NewMulti(
	log.FileConfig{
		Filename:   "logs/app.log",
		MaxSize:    100,
		MaxBackups: 10,
		MaxAge:     30,
		Compression: "gzip",
	},
	log.WithLevel(slog.LevelDebug),
	log.WithAddSource(true),
)
if err != nil {
	panic(err)
}
defer logger.Close()

log.SetDefault(logger)
log.Debug("global logger updated")
```

> `SetDefault` 只原子替换指针且忽略 `nil`，不会自动关闭旧日志器；应用退出前请自行调用当前 `logger.Close()` 刷盘释放句柄。

### 独立实例与文件轮转

```go
console := log.New(
	log.WithLevel(slog.LevelDebug),
	log.WithAddSource(true),
	log.WithTimeFormat("2006-01-02 15:04:05"),
)
console.Info("listening", slog.Int("port", 8080))

fileOnly, err := log.NewFile(log.FileConfig{
	Filename:         "logs/json.log",
	MaxSize:          100,
	RotationInterval: 24 * time.Hour,
})
if err != nil {
	return err
}
defer fileOnly.Close()
```

通过 `logger.Slog()` 可获得底层 `*slog.Logger`，直接接入 `x/transport/http`、`x/transport/grpc`、`x/store/*`、`x/asynq` 等组件。

## 敏感数据脱敏（Redactor）

`Redactor` 基于 `atomic.Pointer` 实现无锁执行计划切换，支持递归遍历 `slog.Group`、`map`、struct 与 `error`；对 `slog.Any` 嵌套对象默认最多递归 16 层，超过深度上限的子树会以 `"[REDACTED:MAX_DEPTH]"` 整体截断，避免深层敏感字段原样泄露：

```go
redactor, err := log.NewRedactor(
	log.FieldRule("password", log.MaskFull("******")),
	log.FieldRule("phone", log.MaskPartial(3, 4)),
	log.ContentRule("email", `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, log.MaskEmail()),
)
if err != nil {
	panic(err)
}

logger := log.New(log.WithRedactor(redactor))
logger.Info("login", slog.String("phone", "13812345678")) // phone=138****5678

// 运行时动态增删启停规则（并发安全）
_ = redactor.AddRule(log.FieldRule("token", log.MaskFull()))
redactor.DisableRule("token")
redactor.EnableRule("token")
redactor.RemoveRule("token")
```

## Context 属性提取

配合 [`x/otel`](file:///root/workspace/kit/x/otel) 等组件从 `context.Context` 自动注入 `trace_id`、`span_id` 等上下文属性：

```go
logger := log.New(
	log.WithContextExtractor(func(ctx context.Context) []slog.Attr {
		return []slog.Attr{slog.String("trace_id", traceIDFrom(ctx))}
	}),
)
logger.InfoContext(ctx, "order created", slog.String("order_id", "ord-1001"))
```

## 选项参考

| 选项 | 说明 | 默认值 |
|---|---|---|
| `WithLevel(slog.Level)` | 设置最低输出级别 | `slog.LevelInfo` |
| `WithAddSource(bool)` | 是否记录调用方文件与行号（caller） | `false` |
| `WithNoColor(bool)` | 禁用控制台 ANSI 颜色 | `false` |
| `WithTimeFormat(string)` | 控制台时间格式 | `time.DateTime` |
| `WithWriter(io.Writer)` | 覆盖控制台输出目标 | `os.Stdout` |
| `WithRedactor(*Redactor)` | 绑定动态脱敏处理器 | 无 |
| `WithContextExtractor(ContextExtractor)` | 注册 Context 属性提取器 | 无 |

## 验证

```bash
go test -v -run TestLogOutputPreview ./log
go test -race -count=1 ./log/...
```
