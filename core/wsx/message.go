package wsx

// MessageType 定义 WebSocket 消息类型，取值兼容 RFC 6455 opcode。
type MessageType int

const (
	// TextMessage 文本消息 (opcode 1)
	TextMessage MessageType = 1
	// BinaryMessage 二进制消息 (opcode 2)
	BinaryMessage MessageType = 2
	// CloseMessage 关闭消息 (opcode 8)
	CloseMessage MessageType = 8
	// PingMessage ping 消息 (opcode 9)
	PingMessage MessageType = 9
	// PongMessage pong 消息 (opcode 10)
	PongMessage MessageType = 10
)

// Message 表示一条 WebSocket 消息。
type Message struct {
	Type MessageType `json:"type"`
	Data []byte      `json:"data"`
}
