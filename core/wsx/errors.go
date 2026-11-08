package wsx

import "errors"

// Sentinel errors that callers can match with errors.Is.
var (
	// ErrClientClosed indicates that the client is closed and cannot be reused.
	ErrClientClosed = errors.New("client closed")
	// ErrAlreadyConnected indicates an active session, including a pending connection or reconnect.
	ErrAlreadyConnected = errors.New("already connected")
	// ErrNotConnected indicates that no server connection is established.
	ErrNotConnected = errors.New("not connected")
	// ErrInvalidScheme indicates a URL scheme other than ws or wss.
	ErrInvalidScheme = errors.New("invalid websocket scheme")
	// ErrInvalidURL indicates a URL parsing failure.
	ErrInvalidURL = errors.New("invalid websocket url")
	// ErrSendTimeout indicates a timeout while enqueueing a message.
	ErrSendTimeout = errors.New("send timeout")
	// ErrMaxRetriesExceeded indicates that the reconnect limit has been reached.
	ErrMaxRetriesExceeded = errors.New("max reconnection attempts reached")
)
