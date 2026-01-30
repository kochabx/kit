package kafka

import (
	"time"

	"github.com/kochabx/kit/core/tag"
	"github.com/segmentio/kafka-go"
)

// Config Kafka 客户端配置
type Config struct {
	// ==================== 连接配置 ====================
	// Brokers Kafka Broker 地址列表
	// 格式: ["localhost:9092"]
	Brokers []string `json:"brokers" default:"localhost:9092"`

	// ==================== 认证配置 ====================
	// Username SASL 用户名
	Username string `json:"username"`

	// Password SASL 密码
	Password string `json:"password"`

	// ==================== 生产/消费配置 ====================
	// Balancer 负载均衡策略
	// 0: LeastBytes (默认)
	// 1: Hash
	Balancer Balancer `json:"balancer" default:"0"`

	// Partition 指定分区 (仅用于 Consumer，ConsumerGroup 忽略)
	Partition int `json:"partition" default:"0"`

	// AllowAutoTopicCreation 是否允许自动创建 Topic
	AllowAutoTopicCreation bool `json:"allowAutoTopicCreation" default:"false"`

	// ==================== 超时配置 ====================
	// Timeout 连接超时时间
	Timeout time.Duration `json:"timeout" default:"3s"`

	// CloseTimeout 关闭超时时间
	CloseTimeout time.Duration `json:"closeTimeout" default:"5s"`

	// ==================== 批处理配置 ====================
	// MinBytes 最小批处理字节数
	MinBytes int `json:"minBytes" default:"1024"` // 1KB

	// MaxBytes 最大批处理字节数
	MaxBytes int `json:"maxBytes" default:"1048576"` // 1MB
}

// Balancer 负载均衡策略枚举
type Balancer int

const (
	BalancerLeastBytes Balancer = iota
	BalancerHash
)

// ApplyDefaults 应用默认值
func (c *Config) ApplyDefaults() error {
	return tag.ApplyDefaults(c)
}

// balancer 获取 kafka-go 的 Balancer 实现
func (c *Config) balancer() kafka.Balancer {
	switch c.Balancer {
	case BalancerLeastBytes:
		return &kafka.LeastBytes{}
	case BalancerHash:
		return &kafka.Hash{}
	default:
		return &kafka.LeastBytes{}
	}
}
