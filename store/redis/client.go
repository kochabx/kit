package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client Redis 客户端通用接口
type Client interface {
	Ping(ctx context.Context) error
	Close() error
	Stats() *redis.PoolStats
}

// NewRedisClient 通用 Redis 客户端创建器
// 根据配置类型自动选择单机或集群模式
func NewRedisClient(config RedisConfig, opts ...any) (Client, error) {
	switch config.ClientType() {
	case ClientSingle:
		singleConfig, ok := config.(*SingleConfig)
		if !ok {
			return nil, fmt.Errorf("got %T, want *SingleConfig", config)
		}

		var singleOpts []SingleOption
		for _, opt := range opts {
			if singleOpt, ok := opt.(SingleOption); ok {
				singleOpts = append(singleOpts, singleOpt)
			}
		}

		return NewClient(singleConfig, singleOpts...)

	case ClientCluster:
		clusterConfig, ok := config.(*ClusterConfig)
		if !ok {
			return nil, fmt.Errorf("got %T, want *ClusterConfig", config)
		}

		var clusterOpts []ClusterOption
		for _, opt := range opts {
			if clusterOpt, ok := opt.(ClusterOption); ok {
				clusterOpts = append(clusterOpts, clusterOpt)
			}
		}

		return NewClusterClient(clusterConfig, clusterOpts...)

	default:
		return nil, ErrUnsupportedClientType
	}
}

// WithSingleOptions 单机模式选项辅助函数
func WithSingleOptions(opts ...SingleOption) []any {
	result := make([]any, len(opts))
	for i, opt := range opts {
		result[i] = opt
	}
	return result
}

// WithClusterOptions 集群模式选项辅助函数
func WithClusterOptions(opts ...ClusterOption) []any {
	result := make([]any, len(opts))
	for i, opt := range opts {
		result[i] = opt
	}
	return result
}

// SingleClient 获取单机客户端的真实实例
func SingleClient(client Client) (*redis.Client, bool) {
	if s, ok := client.(*Single); ok {
		return s.Client, true
	}
	return nil, false
}

// ClusterClient 获取集群客户端的真实实例
func ClusterClient(client Client) (*redis.ClusterClient, bool) {
	if c, ok := client.(*Cluster); ok {
		return c.Client, true
	}
	return nil, false
}
