# gRPC 扩展

本目录目前没有 Go 中间件实现。实际服务封装位于 [transport/grpc](../)，通过原生 gRPC 扩展点配置：

```go
server := kitgrpc.NewServer(
    kitgrpc.WithUnaryInterceptor(unaryInterceptor),
    kitgrpc.WithStreamInterceptor(streamInterceptor),
    kitgrpc.WithServerOption(grpc.StatsHandler(statsHandler)),
)
registerServices(server.Srv())
```

`kitgrpc` 对应 `github.com/kochabx/kit/transport/grpc`，`grpc` 对应 `google.golang.org/grpc`。应用提供 interceptor、StatsHandler 和服务注册函数；在启动前完成注册。

Server 实现 Start/Stop，可交给 `app.WithServer(server)` 管理。`app.New` 返回应用和错误；自定义 TLS 使用 `WithTLSConfig`。OpenTelemetry Provider 和 instrumentation 由应用显式接入，本目录不自动启用它们。

```bash
go test ./transport/grpc/... -run '^$'
```
