package wsx

import "errors"

// Sentinel errors. 调用方可使用 errors.Is 判断。
var (
	// ErrClientClosed 客户端已被关闭，不能再使用。
	ErrClientClosed = errors.New("client closed")
	// ErrAlreadyConnected 连接已建立，重复连接被拒绝。
	ErrAlreadyConnected = errors.New("already connected")
	// ErrNotConnected 当前未连接到服务器。
	ErrNotConnected = errors.New("not connected")
	// ErrInvalidScheme URL scheme 非 ws/wss。
	ErrInvalidScheme = errors.New("invalid websocket scheme")
	// ErrInvalidURL URL 解析失败。
	ErrInvalidURL = errors.New("invalid websocket url")
	// ErrSendTimeout 发送队列阻塞超时。
	ErrSendTimeout = errors.New("send timeout")
	// ErrMaxRetriesExceeded 已达到最大重连次数。
	ErrMaxRetriesExceeded = errors.New("max reconnection attempts reached")
)
