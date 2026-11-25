# x/transport

服务端传输层实现：

- `github.com/kochabx/kit/x/transport/http` — 标准库 `net/http` Server 生命周期、泛型 JSON `Router`、RFC 9457 `Problem` 响应、内置 Scalar OpenAPI 3.x UI 与 7 个安全中间件
- `github.com/kochabx/kit/x/transport/http/gin` — Gin 框架适配器（`Adapt` 完整桥接包装后的 `http.ResponseWriter` 状态，`WriteProblem` 直接输出 RFC 9457 错误）
- `github.com/kochabx/kit/x/transport/http/prom` — Prometheus HTTP 指标中间件与 `/metrics` Handler
- `github.com/kochabx/kit/x/transport/grpc` — gRPC Server（`otelgrpc`、`slog` 日志、Panic 恢复、`*fault.Error` 状态映射、`grpc/health/v1` 与 reflection）

---

## 1. HTTP 路由、Server 与中间件（`x/transport/http`）

```go
import (
	"github.com/kochabx/kit/app"
	kithttp "github.com/kochabx/kit/x/transport/http"
	"github.com/kochabx/kit/x/validate"
)

v, _ := validate.New()
rt := kithttp.NewRouter(kithttp.WithValidator(v))

rt.Handle("POST /v1/orders", func(ctx context.Context, req CreateOrderReq) (OrderResp, error) {
	return createOrder(ctx, req)
})

var handler http.Handler = rt
handler = kithttp.Logger(logger.Slog())(handler)
handler = kithttp.Recovery(logger.Slog())(handler)

srv := kithttp.NewServer(handler,
	kithttp.WithAddr(":8080"),
	kithttp.WithHealthEndpoint("/healthz", container),
	kithttp.WithOpenAPI(kithttp.OpenAPIConfig{Spec: openAPISpec}),
)
```

`Router.Handle[Req, Resp]` 自动限流读取并解码 JSON 请求体、执行 `Validator` 校验，并输出统一 `Envelope[Resp]` 或 RFC 9457 `Problem` 响应。

内置中间件与辅助函数：
- 中间件：`Recovery`、`Logger`、`CORS`、`CSRF`、`Auth`、`Permission`、`Crypto`、`Signature`
- 响应与健康检查：`WriteJSON`、`WriteOK`、`WriteProblem`、`HealthHandler`

---

## 2. Gin 适配（`x/transport/http/gin`）

```go
r := gin.New()
r.Use(kitgin.Adapt(kithttp.Recovery(logger.Slog())))
r.Use(kitgin.Adapt(kithttp.Logger(logger.Slog())))
```

`Adapt` 通过双向桥接完整保留下游/上游对 `http.ResponseWriter`（如响应状态码、流式 `Flush`、Context 注入）的变更；handler 内可直接调用 `kitgin.WriteProblem(c, err)` 输出 RFC 9457 错误。

---

## 3. Prometheus 指标（`x/transport/http/prom`）

```go
mw, err := prom.Middleware(prom.WithNamespace("myapp"))
if err != nil {
	return err
}
rt.HandleRaw("GET /metrics", prom.Handler(prometheus.DefaultGatherer))
```

---

## 4. gRPC Server（`x/transport/grpc`）

```go
gsrv := kitgrpc.NewServer(
	kitgrpc.WithAddr(":9090"),
	kitgrpc.WithLogger(logger.Slog()),
	kitgrpc.WithHealthServer(true),
	kitgrpc.WithReflection(false),
)
// 注册 gRPC Service 到 gsrv.GRPCServer()，再交给 app.WithServers(gsrv) 托管启停
```

## 验证

```bash
go test -race -count=1 ./x/transport/...
```
