# WebSocket 客户端

一个功能丰富、生产就绪的 Go WebSocket 客户端库，支持自动重连、事件驱动、ping/pong 心跳检测等特性。

## 特性

- ✅ **简单易用**: 提供简洁的 API 接口，支持链式配置
- ✅ **自动重连**: 支持指数退避的智能重连机制
- ✅ **事件驱动**: 基于事件的消息处理系统
- ✅ **心跳检测**: 内置 ping/pong 机制保持连接活跃
- ✅ **并发安全**: 线程安全的设计，支持多 goroutine 使用
- ✅ **灵活配置**: 支持超时、缓冲区大小、压缩等多种配置项
- ✅ **TLS 支持**: 支持 WSS 安全连接
- ✅ **错误处理**: 完善的错误处理和事件通知机制

## 安装

```bash
go get github.com/kochabx/kit/transport/websocket
```

## 快速开始

### 基础用法

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/kochabx/kit/transport/websocket"
)

func main() {
    // 创建 WebSocket 客户端
    client := websocket.NewClient()
    defer client.Close()
    
    // 注册事件处理器
    client.OnEvent(websocket.EventConnected, func(event websocket.Event) {
        log.Println("✅ 连接成功")
    })
    
    client.OnEvent(websocket.EventMessage, func(event websocket.Event) {
        if msg, ok := event.Data.(websocket.Message); ok {
            log.Printf("📩 收到消息: %s", string(msg.Data))
        }
    })
    
    client.OnEvent(websocket.EventError, func(event websocket.Event) {
        log.Printf("❌ 错误: %v", event.Error)
    })
    
    // 连接到 WebSocket 服务器
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    err := client.Connect(ctx, "wss://echo.websocket.org")
    if err != nil {
        log.Fatal(err)
    }
    
    // 发送消息
    err = client.SendText("Hello WebSocket!")
    if err != nil {
        log.Printf("发送消息失败: %v", err)
    }
    
    // 等待一段时间
    time.Sleep(5 * time.Second)
}
```

### 高级配置

```go
// 创建带自定义配置的客户端
client := websocket.NewClient(
    websocket.WithConnectTimeout(30*time.Second),
    websocket.WithReadTimeout(60*time.Second),
    websocket.WithWriteTimeout(10*time.Second),
    websocket.WithPingInterval(30*time.Second),
    websocket.WithMaxMessageSize(2*1024*1024), // 2MB
    websocket.WithHeaders(http.Header{
        "Authorization": []string{"Bearer your-token"},
        "User-Agent":    []string{"MyApp/1.0"},
    }),
    websocket.WithReconnectConfig(websocket.ReconnectConfig{
        Enable:            true,
        MaxRetries:        10,
        Interval:          2 * time.Second,
        MaxInterval:       60 * time.Second,
        BackoffMultiplier: 2.0,
    }),
)
```

## API 参考

### 客户端接口

```go
type Client interface {
    // 连接到 WebSocket 服务器
    Connect(ctx context.Context, url string) error
    
    // 断开连接
    Disconnect() error
    
    // 发送原始消息
    Send(messageType MessageType, data []byte) error
    
    // 发送文本消息
    SendText(text string) error
    
    // 发送二进制消息
    SendBinary(data []byte) error
    
    // 注册事件处理器
    OnEvent(eventType EventType, handler EventHandler)
    
    // 移除事件处理器
    RemoveEventHandler(eventType EventType)
    
    // 检查连接状态
    IsConnected() bool
    
    // 获取配置信息
    GetConfig() Config
    
    // 关闭客户端
    Close() error
}
```

### 消息类型

```go
const (
    TextMessage   MessageType = 1  // 文本消息
    BinaryMessage MessageType = 2  // 二进制消息
    CloseMessage  MessageType = 8  // 关闭消息
    PingMessage   MessageType = 9  // Ping 消息
    PongMessage   MessageType = 10 // Pong 消息
)
```

### 事件类型

```go
const (
    EventConnected     EventType = "connected"     // 连接成功
    EventDisconnected  EventType = "disconnected"  // 连接断开
    EventMessage       EventType = "message"       // 收到消息
    EventError         EventType = "error"         // 发生错误
    EventReconnecting  EventType = "reconnecting"  // 重连中
)
```

## 配置选项

### 基础配置

| 选项 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ConnectTimeout` | `time.Duration` | `30s` | 连接超时时间 |
| `ReadTimeout` | `time.Duration` | `60s` | 读取超时时间 |
| `WriteTimeout` | `time.Duration` | `10s` | 写入超时时间 |
| `PingInterval` | `time.Duration` | `54s` | Ping 发送间隔 |
| `PongWait` | `time.Duration` | `60s` | Pong 等待时间 |
| `MaxMessageSize` | `int64` | `1MB` | 最大消息大小 |
| `ReadBufferSize` | `int` | `4096` | 读缓冲区大小 |
| `WriteBufferSize` | `int` | `4096` | 写缓冲区大小 |
| `EnableCompression` | `bool` | `false` | 是否启用压缩 |

### 重连配置

| 选项 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Enable` | `bool` | `true` | 是否启用自动重连 |
| `MaxRetries` | `int` | `5` | 最大重连次数 (0=无限) |
| `Interval` | `time.Duration` | `1s` | 初始重连间隔 |
| `MaxInterval` | `time.Duration` | `30s` | 最大重连间隔 |
| `BackoffMultiplier` | `float64` | `2.0` | 退避倍数 |

## 使用场景

### 实时聊天应用

```go
client := websocket.NewClient()

// 处理收到的聊天消息
client.OnEvent(websocket.EventMessage, func(event websocket.Event) {
    if msg, ok := event.Data.(websocket.Message); ok {
        // 解析聊天消息
        var chatMsg ChatMessage
        json.Unmarshal(msg.Data, &chatMsg)
        
        // 显示消息
        fmt.Printf("[%s]: %s\n", chatMsg.Username, chatMsg.Content)
    }
})

// 发送聊天消息
func sendMessage(content string) error {
    msg := ChatMessage{
        Username: "current_user",
        Content:  content,
        Time:     time.Now(),
    }
    
    data, _ := json.Marshal(msg)
    return client.SendText(string(data))
}
```

### 实时数据订阅

```go
client := websocket.NewClient(
    websocket.WithReconnectConfig(websocket.ReconnectConfig{
        Enable:     true,
        MaxRetries: 0, // 无限重连
        Interval:   5 * time.Second,
    }),
)

// 处理市场数据
client.OnEvent(websocket.EventMessage, func(event websocket.Event) {
    if msg, ok := event.Data.(websocket.Message); ok {
        var marketData MarketData
        json.Unmarshal(msg.Data, &marketData)
        
        // 更新本地数据
        updateLocalData(marketData)
    }
})

// 连接后订阅数据流
client.OnEvent(websocket.EventConnected, func(event websocket.Event) {
    // 订阅特定的数据流
    subscribeMsg := map[string]any{
        "action":  "subscribe",
        "streams": []string{"btcusdt@ticker", "ethusdt@ticker"},
    }
    
    data, _ := json.Marshal(subscribeMsg)
    client.SendText(string(data))
})
```

### 心跳监控

```go
client := websocket.NewClient(
    websocket.WithPingInterval(30*time.Second),
    websocket.WithPongWait(35*time.Second),
)

// 监控连接状态
client.OnEvent(websocket.EventDisconnected, func(event websocket.Event) {
    log.Println("连接断开，原因:", event.Error)
    
    // 执行清理操作
    cleanup()
})

client.OnEvent(websocket.EventReconnecting, func(event websocket.Event) {
    log.Println("正在尝试重连...")
    
    // 显示重连状态
    showReconnectingStatus()
})
```

## 错误处理

### 常见错误类型

```go
client.OnEvent(websocket.EventError, func(event websocket.Event) {
    switch {
    case strings.Contains(event.Error.Error(), "connection refused"):
        log.Println("服务器拒绝连接")
        // 处理连接被拒绝的情况
        
    case strings.Contains(event.Error.Error(), "timeout"):
        log.Println("连接超时")
        // 处理超时情况
        
    case strings.Contains(event.Error.Error(), "certificate"):
        log.Println("TLS 证书错误")
        // 处理证书问题
        
    default:
        log.Printf("未知错误: %v", event.Error)
    }
})
```

### 手动错误处理

```go
// 发送消息时的错误处理
err := client.SendText("Hello")
if err != nil {
    switch {
    case errors.Is(err, websocket.ErrCloseSent):
        log.Println("连接已关闭")
    case strings.Contains(err.Error(), "write timeout"):
        log.Println("写入超时")
    default:
        log.Printf("发送失败: %v", err)
    }
}
```

## 最佳实践

### 1. 资源管理

```go
// 总是在使用完毕后关闭客户端
defer client.Close()

// 或者使用 context 控制生命周期
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// 在适当的时候调用 cancel 来停止所有操作
```

### 2. 事件处理器管理

```go
// 避免在事件处理器中执行耗时操作
client.OnEvent(websocket.EventMessage, func(event websocket.Event) {
    // 快速处理，或者启动 goroutine
    go func() {
        // 耗时的业务逻辑
        processMessage(event.Data)
    }()
})
```

### 3. 错误重试

```go
// 在关键业务中实现自定义重试逻辑
func sendMessageWithRetry(client websocket.Client, msg string, maxRetries int) error {
    for i := 0; i < maxRetries; i++ {
        err := client.SendText(msg)
        if err == nil {
            return nil
        }
        
        log.Printf("发送失败 (尝试 %d/%d): %v", i+1, maxRetries, err)
        time.Sleep(time.Duration(i+1) * time.Second)
    }
    
    return fmt.Errorf("发送消息失败，已重试 %d 次", maxRetries)
}
```

### 4. 连接状态检查

```go
// 在发送重要消息前检查连接状态
if !client.IsConnected() {
    log.Println("连接未建立，尝试重新连接...")
    
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if err := client.Connect(ctx, wsURL); err != nil {
        return fmt.Errorf("重连失败: %w", err)
    }
}
```

## 测试

运行测试：

```bash
# 运行所有测试
go test ./...

# 运行单元测试（跳过集成测试）
go test -short ./...

# 运行集成测试
go test -v ./... -run TestPostmanEcho
```