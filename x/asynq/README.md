# x/asynq

基于 `github.com/hibiken/asynq` 的超薄调度胶水，统一配置校验、结构化 `slog` 日志桥接、OpenTelemetry 中间件及 `cx` 生命周期管理，直接暴露原生 `*asynq.Client` / `*asynq.Server` / `*asynq.Scheduler`。

## 快速开始

```go
import (
	"github.com/hibiken/asynq"

	kitasynq "github.com/kochabx/kit/x/asynq"
)

cfg := kitasynq.Config{
	RedisClient: rdb.Client(), // 或 RedisOpt: asynq.RedisClientOpt{Addr: "127.0.0.1:6379"}
	Concurrency: 10,
	Queues:      map[string]int{"critical": 6, "default": 3, "low": 1},
}

client, err := kitasynq.NewClient(cfg)
if err != nil {
	return err
}
defer client.Close()

mux := asynq.NewServeMux()
srv, err := kitasynq.NewServer(cfg, mux,
	kitasynq.WithLogger(logger.Slog()),
)
if err != nil {
	return err
}
```

`Server` 和 `Scheduler` 均实现 `Run(ctx)`、`Start(ctx)`、`Stop(ctx)` 与 `HealthCheck(ctx)`，可直接配合 `cx.NewRunner` / `app.WithRunnable` 使用；同时内置链路追踪中间件，可通过 `WithOTelOptions` 自定义或通过 `cfg.EnableTracing` 关闭。

## 常用 API

- `NewClient(cfg)` → `.Client() *asynq.Client`
- `NewServer(cfg, handler, opts...)` → `.Server() *asynq.Server`
- `NewScheduler(cfg, opts...)` → `.Scheduler() *asynq.Scheduler`
- `OTelMiddleware(opts...)`：独立的 OpenTelemetry 任务追踪/指标中间件
- 选项：`WithLogger`、`WithMiddleware`、`WithOTelOptions`、`WithServerConfigHook`、`WithSchedulerOptsHook`

## 验证

```bash
go test -race -count=1 ./x/asynq/...
```
