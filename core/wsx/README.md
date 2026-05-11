# wsx — WebSocket 客户端

一个生产级 Go WebSocket 客户端：自动重连、事件驱动、心跳保活，命名风格对齐 [`httpx`](../httpx/)。

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
    if err := client.Connect(ctx, "wss://ws.postman-echo.com/raw"); err != nil {
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

```go
var (
    ErrClientClosed       // 客户端已 Close
    ErrAlreadyConnected   // 已建立连接
    ErrNotConnected       // 当前未连接
    ErrInvalidScheme      // scheme 非 ws/wss
    ErrInvalidURL         // URL 解析失败
    ErrSendTimeout        // Send 入队超时
    ErrMaxRetriesExceeded // 触达最大重连次数
)
```

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

## 设计要点（v2 重构说明）

- **接口/实现分离**：导出 `*Client` 实现 + `Clienter` 接口，对齐 `httpx`。
- **死锁修复**：v1 的 `Disconnect/scheduleReconnect` 在持锁状态下调用 `emitEvent` / `connect`，与 `emitEvent` 内的 `RLock` 互斥导致死锁；v2 改为复制必要状态后释放锁再回调。
- **conn 所有权**：每条连接的 `readLoop/writeLoop/pingLoop` 通过参数持有自己的 `*websocket.Conn` 与 `context`，重连后旧 conn 不会被新循环误用。
- **ping 独立通道**：心跳通过 `WriteControl` 直接发送，不再与业务消息共用 `writeChan`。
- **资源安全**：`Close` 用 `sync.Once` 守护可重复调用；`Send` 用 `time.NewTimer + defer Stop` 避免 timer 泄漏。
- **状态机**：新增 `intentionalDisconnect` 标志，`Disconnect` 不会触发自动重连。

## 旧版迁移指引

| v1 | v2 |
|---|---|
| `NewClient(...)` | `New(...)` |
| `wsClientOption` | `Option` |
| `client.(*wsClient).dialer.TLSClientConfig = ...` | `WithTLSConfig(...)` |
| `WithConnectTimeout` | `WithHandshakeTimeout` |
| `WithReconnectConfig` | `WithReconnect` |
| `Config.ConnectTimeout` | `Config.HandshakeTimeout` |
| `Config.ReconnectConfig` | `Config.Reconnect` |
| `Client.GetConfig()` | `Client.Config()` |
| 实现类型 `wsClient`（未导出） | 实现类型 `Client`（导出） |
| 接口名 `Client` | 接口名 `Clienter` |
