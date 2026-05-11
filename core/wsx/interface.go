package wsx

import "context"

// Clienter WebSocket 客户端接口，便于在调用方进行 mock。
type Clienter interface {
	// Connect 连接到指定的 WebSocket URL，scheme 必须为 ws 或 wss。
	Connect(ctx context.Context, url string) error
	// Disconnect 主动断开当前连接，但保留客户端以便后续 Connect。
	// 该次断开不会触发自动重连。
	Disconnect() error
	// Close 关闭客户端，释放全部资源；之后 Connect 将返回 ErrClientClosed。
	Close() error

	// Send 发送一条消息，由 messageType 决定帧类型。
	Send(messageType MessageType, data []byte) error
	// SendText 发送文本消息。
	SendText(text string) error
	// SendBinary 发送二进制消息。
	SendBinary(data []byte) error

	// OnEvent 注册事件回调，可对同一事件类型注册多个回调。
	OnEvent(eventType EventType, handler EventHandler)
	// RemoveEventHandler 移除指定事件类型下的所有回调。
	RemoveEventHandler(eventType EventType)

	// IsConnected 返回当前是否已连接。
	IsConnected() bool
	// Config 返回当前配置的副本。
	Config() Config
}
