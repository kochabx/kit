package wsx

// MessageType identifies a WebSocket frame type using RFC 6455 opcodes.
type MessageType int

const (
	// TextMessage is a text frame (opcode 1).
	TextMessage MessageType = 1
	// BinaryMessage is a binary frame (opcode 2).
	BinaryMessage MessageType = 2
	// CloseMessage is a close frame (opcode 8).
	CloseMessage MessageType = 8
	// PingMessage is a ping frame (opcode 9).
	PingMessage MessageType = 9
	// PongMessage is a pong frame (opcode 10).
	PongMessage MessageType = 10
)

// Message describes a WebSocket message.
type Message struct {
	Type MessageType `json:"type"`
	Data []byte      `json:"data"`
}
