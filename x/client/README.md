# x/client

出站网络客户端合集，包含 RESTful HTTP 客户端与可自动重连的 WebSocket 客户端：

- `github.com/kochabx/kit/x/client/http` — 基于 `resty.dev/v3`、`otelhttp` 与 `*fault.Error` 错误映射的泛型 JSON 客户端
- `github.com/kochabx/kit/x/client/ws` — 基于 `coder/websocket`、`cenkalti/backoff/v5` 与 `iter.Seq2` 的保序 WebSocket 客户端

---

## 1. HTTP 客户端（`x/client/http`）

```go
import kithttpclient "github.com/kochabx/kit/x/client/http"

client, err := kithttpclient.New(
	kithttpclient.WithBaseURL("https://api.example.com"),
	kithttpclient.WithTimeout(5*time.Second),
	kithttpclient.WithRetry(2, 100*time.Millisecond, time.Second),
	kithttpclient.WithOTel(true),
)
if err != nil {
	return err
}
defer client.Close()

order, err := client.Post[CreateOrderReq, OrderResp](ctx, "/orders", req,
	kithttpclient.WithRequestHeader("X-Idempotency-Key", idemKey),
)
```

- 常用泛型方法：`(*Client).Get[Resp]`、`(*Client).Post[Req, Resp]`、`(*Client).Put[Req, Resp]`、`(*Client).Delete[Resp]`；同时支持 `client.Resty()` 访问原生 `*resty.Client`；
- 非 2xx 响应自动映射为结构化 `*fault.Error`（保留上游状态码）。

---

## 2. WebSocket 客户端（`x/client/ws`）

```go
import "github.com/kochabx/kit/x/client/ws"

c, err := ws.New("wss://stream.example.com/ws",
	ws.WithDialTimeout(5*time.Second),
	ws.WithPingInterval(20*time.Second),
	ws.WithReconnect(true, 200*time.Millisecond, 5*time.Second, 0),
)
if err != nil {
	return err
}
defer c.Close()

if err := c.Connect(ctx); err != nil {
	return err
}

for msg, err := range c.Messages(ctx) {
	if err != nil {
		return err
	}
	_ = msg.Data
}
```

支持指数退避断线重连、心跳探活、`Send(ctx, ws.Message{Type: ws.MessageText, Data: ...})` 及按到达顺序产出的 `iter.Seq2[Message, error]`。

## 验证

```bash
go test -race -count=1 ./x/client/...
```
