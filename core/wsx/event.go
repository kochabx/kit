package wsx

import "time"

// EventType identifies a WebSocket event.
type EventType string

const (
	// EventConnected indicates a successful connection.
	EventConnected EventType = "connected"
	// EventDisconnected indicates a disconnected connection.
	EventDisconnected EventType = "disconnected"
	// EventMessage indicates an incoming message.
	EventMessage EventType = "message"
	// EventError indicates an error.
	EventError EventType = "error"
	// EventReconnecting indicates a reconnect attempt.
	EventReconnecting EventType = "reconnecting"
)

// Event describes a WebSocket event.
type Event struct {
	Type      EventType `json:"type"`
	Data      any       `json:"data,omitempty"`
	Error     error     `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler handles a WebSocket event.
type EventHandler func(event Event)
