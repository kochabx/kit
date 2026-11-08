package wsx

import "context"

// Clienter exposes WebSocket operations and can be mocked by callers.
type Clienter interface {
	// Connect creates a session controlled by ctx, covering handshakes, connection I/O, and reconnects.
	// Cancellation disconnects and stops reconnects without closing the client; a new Context can start another session.
	// The URL scheme must be ws or wss. A nil ctx uses the client's own Context.
	Connect(ctx context.Context, url string) error
	// Disconnect ends the active session while keeping the client available for another Connect.
	// An explicit disconnect does not trigger automatic reconnection.
	Disconnect() error
	// Close releases client resources; subsequent Connect calls return ErrClientClosed.
	Close() error

	// Send enqueues a frame of the specified messageType.
	Send(messageType MessageType, data []byte) error
	// SendText sends a text message.
	SendText(text string) error
	// SendBinary sends a binary message.
	SendBinary(data []byte) error

	// OnEvent adds a handler; each event type may have multiple handlers.
	OnEvent(eventType EventType, handler EventHandler)
	// RemoveEventHandler removes all handlers for an event type.
	RemoveEventHandler(eventType EventType)

	// IsConnected reports whether a connection is currently established.
	IsConnected() bool
	// Config returns a copy of the current configuration.
	Config() Config
}
