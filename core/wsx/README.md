# wsx — WebSocket 客户端

Go WebSocket 客户端：自动重连、事件驱动、心跳保活，命名风格对齐 [`httpx`](../httpx/)。

`wsx` 属于 `core` 通用能力：它是出站客户端工具，不承担 `transport` 服务端协议接入职责。

## 特性

- **简单易用**：`New(...Option)` 一行创建客户端
- **自动重连**：指数退避，最大间隔可控
- **事件驱动**：connected / disconnected / message / error / reconnecting
- **心跳保活**：基于 `WriteControl` 直发 ping，不与业务消息争用写队列
- **并发安全**：所有公开方法可在任意 goroutine 调用
- **TLS 支持**：`WithTLSConfig` 一行启用
- **Sentinel Errors**：`errors.Is(err, wsx.ErrNotConnected)` 等

## 安装

```bash
go get github.com/kochabx/kit/core/wsx
```

## 快速开始

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/kochabx/kit/core/wsx"
)

func main() {
    client := wsx.New()
    defer client.Close()

    client.OnEvent(wsx.EventConnected, func(e wsx.Event) {
        log.Println("✅ 连接成功")
    })
    client.OnEvent(wsx.EventMessage, func(e wsx.Event) {
        if msg, ok := e.Data.(wsx.Message); ok {
            log.Printf("📩 %s", msg.Data)
        }
    })
    client.OnEvent(wsx.EventError, func(e wsx.Event) {
        log.Printf("❌ %v", e.Error)
    })

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := client.Connect(ctx, "ws://127.0.0.1:8080/ws"); err != nil {
        log.Fatal(err)
    }

    _ = client.SendText("hello")
    time.Sleep(2 * time.Second)
}
```

## 高级配置

```go
client := wsx.New(
    wsx.WithHandshakeTimeout(30*time.Second),
    wsx.WithReadTimeout(60*time.Second),
    wsx.WithWriteTimeout(10*time.Second),
    wsx.WithPingInterval(30*time.Second),
    wsx.WithMaxMessageSize(2*1024*1024),
    wsx.WithWriteQueueSize(256),
    wsx.WithHeaders(http.Header{
        "Authorization": []string{"Bearer xxx"},
    }),
    wsx.WithTLSConfig(&tls.Config{InsecureSkipVerify: false}),
    wsx.WithReconnect(wsx.ReconnectConfig{
        Enable:            true,
        MaxRetries:        10,
        Interval:          2 * time.Second,
        MaxInterval:       60 * time.Second,
        BackoffMultiplier: 2.0,
    }),
)
```

## API 总览

```go
type Clienter interface {
    Connect(ctx context.Context, url string) error
    Disconnect() error
    Close() error

    Send(messageType MessageType, data []byte) error
    SendText(text string) error
    SendBinary(data []byte) error

    OnEvent(eventType EventType, handler EventHandler)
    RemoveEventHandler(eventType EventType)

    IsConnected() bool
    Config() Config
}

func New(opts ...Option) *Client
```

### 消息类型

| 常量 | 值 | 说明 |
|---|---|---|
| `TextMessage` | 1 | 文本帧 |
| `BinaryMessage` | 2 | 二进制帧 |
| `CloseMessage` | 8 | 关闭帧 |
| `PingMessage` | 9 | Ping 帧 |
| `PongMessage` | 10 | Pong 帧 |

### 事件类型

| 常量 | 触发时机 |
|---|---|
| `EventConnected` | 握手成功 |
| `EventDisconnected` | 连接断开（含主动和被动） |
| `EventMessage` | 收到业务消息（`event.Data` 为 `Message`） |
| `EventError` | 读/写/握手/重连失败 |
| `EventReconnecting` | 进入重连等待，`event.Data` 含 `attempt` 与 `delay` |

### Sentinel Errors

可使用 `errors.Is` 判断：

| 错误 | 含义 |
|---|---|
| `ErrClientClosed` | 客户端已经关闭 |
| `ErrAlreadyConnected` | 已连接或正在重连 |
| `ErrNotConnected` | 尚未连接 |
| `ErrInvalidScheme` | URL scheme 不是 ws/wss |
| `ErrInvalidURL` | URL 解析失败 |
| `ErrSendTimeout` | 发送入队超时 |
| `ErrMaxRetriesExceeded` | 已达重连次数上限 |


## 配置项

### Config

| 字段 | 默认 | 说明 |
|---|---|---|
| `HandshakeTimeout` | `30s` | 握手超时 |
| `ReadTimeout` | `60s` | 单次读取超时 |
| `WriteTimeout` | `10s` | 单次写入超时 |
| `PingInterval` | `54s` | 主动 ping 间隔；`<=0` 关闭 |
| `PongWait` | `60s` | 等待 pong 的最长时间 |
| `MaxMessageSize` | `1MB` | 单条消息最大字节数 |
| `ReadBufferSize` | `4096` | 读缓冲区 |
| `WriteBufferSize` | `4096` | 写缓冲区 |
| `WriteQueueSize` | `128` | 写队列长度 |
| `EnableCompression` | `false` | 是否启用 permessage-deflate |

### ReconnectConfig

| 字段 | 默认 | 说明 |
|---|---|---|
| `Enable` | `true` | 是否启用 |
| `MaxRetries` | `5` | 最大重试次数；`0` 表示无限 |
| `Interval` | `1s` | 初始间隔 |
| `MaxInterval` | `30s` | 间隔上限 |
| `BackoffMultiplier` | `2.0` | 指数退避倍数 |

## 生命周期与回调

`Disconnect` 主动断开且不触发自动重连；`Close` 终止客户端并可重复调用，关闭后不能复用为新客户端。`Connect` 的 Context 控制整个连接会话：握手、已建立连接和自动重连。取消或到期会关闭连接并终止重连；客户端本身仍可通过新的 Context 再次 `Connect`。`HandshakeTimeout` 单独限制握手耗时，不必为了握手超时给整个会话设置短期限。

读、写和心跳循环持有各自连接及 Context，避免重连后旧循环误操作新连接。心跳使用控制帧，不占业务写队列。事件回调应快速返回，业务侧自行同步共享状态；不要在回调内执行阻塞的生命周期等待。

`WithConfig` 替换整份配置，建议从 `wsx.DefaultConfig()` 修改后传入。`WithHeaders`、自定义 Dialer 和 TLS 配置应在连接前固定，运行中不要并发修改它们。自定义拨号使用 `NetDialContext` 或 `NetDialTLSContext`，不再调用旧 `NetDial` 字段。

## 验证

```bash
go test -race -short ./core/wsx -count=1
```

`-short` 运行本地 WebSocket 回归测试，覆盖握手取消、连接取消、重连等待取消、会话隔离和客户端复用，需要允许监听本地端口。去掉 `-short` 还会执行依赖外部 Postman Echo 的网络测试。
