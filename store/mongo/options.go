package mongo

import "github.com/kochabx/kit/log"

// clientOptions 客户端配置选项
type clientOptions struct {
	logger *log.Logger
}

// Option 配置选项函数类型
type Option func(*clientOptions)

// WithLogger 设置自定义日志记录器
func WithLogger(l *log.Logger) Option {
	return func(o *clientOptions) {
		o.logger = l
	}
}
