package wsx

import "time"

// EventType WebSocket 事件类型。
type EventType string

const (
	// EventConnected 连接成功
	EventConnected EventType = "connected"
	// EventDisconnected 连接断开
	EventDisconnected EventType = "disconnected"
	// EventMessage 收到消息
	EventMessage EventType = "message"
	// EventError 发生错误
	EventError EventType = "error"
	// EventReconnecting 正在重连
	EventReconnecting EventType = "reconnecting"
)

// Event WebSocket 事件结构。
type Event struct {
	Type      EventType `json:"type"`
	Data      any       `json:"data,omitempty"`
	Error     error     `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler 事件处理回调。
type EventHandler func(event Event)
