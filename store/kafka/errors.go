package kafka

import "errors"

var (
	// ErrClientClosed 客户端已关闭
	ErrClientClosed = errors.New("kafka: client is closed")

	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = errors.New("kafka: invalid config")

	// ErrEmptyBrokers Broker地址为空
	ErrEmptyBrokers = errors.New("kafka: empty brokers")
)
