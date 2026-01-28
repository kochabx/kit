package kafka

import (
	"github.com/kochabx/kit/log"
	"github.com/segmentio/kafka-go"
)

// Option 客户端配置选项
type Option func(*clientOptions)

// clientOptions 客户端内部选项
type clientOptions struct {
	logger *log.Logger
	dialer *kafka.Dialer
}

// WithLogger 设置日志实例
func WithLogger(logger *log.Logger) Option {
	return func(o *clientOptions) {
		o.logger = logger
	}
}

// WithDialer 设置自定义 Dialer
func WithDialer(dialer *kafka.Dialer) Option {
	return func(o *clientOptions) {
		o.dialer = dialer
	}
}

// applyOptions 应用所有选项
func applyOptions(opts []Option) *clientOptions {
	clientOpts := &clientOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(clientOpts)
		}
	}
	return clientOpts
}
