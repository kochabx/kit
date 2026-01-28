package kafka

import (
	"time"

	"github.com/segmentio/kafka-go"
)

// Option 客户端配置选项
type Option func(*clientOptions)

// clientOptions 客户端内部选项
type clientOptions struct {
	// 基础配置覆盖
	brokers                []string
	username               string
	password               string
	balancer               Balancer
	partition              int
	allowAutoTopicCreation bool

	// 超时配置
	timeout      time.Duration
	closeTimeout time.Duration

	// 批处理配置
	minBytes int
	maxBytes int

	// DialHook
	dialer *kafka.Dialer
}

// WithBrokers 设置 Broker 地址
func WithBrokers(brokers ...string) Option {
	return func(o *clientOptions) {
		o.brokers = brokers
	}
}

// WithAuth 设置认证信息
func WithAuth(username, password string) Option {
	return func(o *clientOptions) {
		o.username = username
		o.password = password
	}
}

// WithBalancer 设置负载均衡策略
func WithBalancer(balancer Balancer) Option {
	return func(o *clientOptions) {
		o.balancer = balancer
	}
}

// WithPartition 设置分区 (仅消费者有效)
func WithPartition(partition int) Option {
	return func(o *clientOptions) {
		o.partition = partition
	}
}

// WithAllowAutoTopicCreation 设置是否允许自动创建 Topic
func WithAllowAutoTopicCreation(allow bool) Option {
	return func(o *clientOptions) {
		o.allowAutoTopicCreation = allow
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(o *clientOptions) {
		o.timeout = timeout
	}
}

// WithCloseTimeout 设置关闭超时时间
func WithCloseTimeout(timeout time.Duration) Option {
	return func(o *clientOptions) {
		o.closeTimeout = timeout
	}
}

// WithBatchBytes 设置批处理大小
func WithBatchBytes(min, max int) Option {
	return func(o *clientOptions) {
		o.minBytes = min
		o.maxBytes = max
	}
}

// WithDialer 设置自定义 Dialer
func WithDialer(dialer *kafka.Dialer) Option {
	return func(o *clientOptions) {
		o.dialer = dialer
	}
}

// applyOptions 应用所有选项到配置
func applyOptions(cfg *Config, opts []Option) *clientOptions {
	clientOpts := &clientOptions{}

	// 应用所有选项
	for _, opt := range opts {
		if opt != nil {
			opt(clientOpts)
		}
	}

	// 将选项值应用到配置
	if len(clientOpts.brokers) > 0 {
		cfg.Brokers = clientOpts.brokers
	}
	if clientOpts.username != "" {
		cfg.Username = clientOpts.username
	}
	if clientOpts.password != "" {
		cfg.Password = clientOpts.password
	}
	if clientOpts.balancer != 0 {
		cfg.Balancer = clientOpts.balancer
	}
	if clientOpts.partition != 0 {
		cfg.Partition = clientOpts.partition
	}
	if clientOpts.allowAutoTopicCreation {
		cfg.AllowAutoTopicCreation = clientOpts.allowAutoTopicCreation
	}
	if clientOpts.timeout > 0 {
		cfg.Timeout = clientOpts.timeout
	}
	if clientOpts.closeTimeout > 0 {
		cfg.CloseTimeout = clientOpts.closeTimeout
	}
	if clientOpts.minBytes > 0 {
		cfg.MinBytes = clientOpts.minBytes
	}
	if clientOpts.maxBytes > 0 {
		cfg.MaxBytes = clientOpts.maxBytes
	}

	return clientOpts
}
