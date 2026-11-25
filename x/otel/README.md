# x/otel

按需初始化的 OpenTelemetry Trace/Metric SDK 封装（OTLP HTTP），提供受管生命周期与 `kit/log` 上下文链路追踪桥接。

## 快速开始

```go
import (
	"github.com/kochabx/kit/log"
	"github.com/kochabx/kit/x/otel"
)

provider, err := otel.New(otel.Config{
	ServiceName:    "order-service",
	ServiceVersion: "v1.0.0",
	Environment:    "prod",
	Endpoint:       "otel-collector:4318",
	Insecure:       true,
})
if err != nil {
	return err
}

if err := provider.Start(ctx); err != nil {
	return err
}
defer provider.Stop(context.Background())

// 配合 kit/log 自动向日志注入 trace_id / span_id
logger := log.New(log.WithContextExtractor(otel.LogExtractor()))
```

## 特性说明

- `otel.New` 默认同步将 `TracerProvider` / `MeterProvider` 及 W3C `TraceContext + Baggage` Propagator 注册到全局（可通过 `WithRegisterGlobal(false)` 关闭，改由 `provider.TracerProvider()` / `provider.MeterProvider()` 手动注入）；
- 实现 `cx.Starter`、`cx.Stopper`、`cx.HealthChecker` 接口，可直接纳入 `cx.Container` 或 `app.WithComponent` 管理；
- `Endpoint` 为空时不连接外部 Collector，仅初始化本地可用 Provider；可通过 `EnableTrace` / `EnableMetric` 按需单独关闭某一项导出，通过 `SampleRatio` 设置采样率（`0`~`1`）。

## 验证

```bash
go test -race -count=1 ./x/otel/...
```
