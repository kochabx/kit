package redis

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrUnsupportedClientType = errors.New("unsupported redis client type")
	ErrClientNotInitialized  = errors.New("redis client not initialized")
)

// Single Redis 单机客户端包装器
type Single struct {
	Client *redis.Client
	config *SingleConfig
}

// Cluster Redis 集群客户端包装器
type Cluster struct {
	Client *redis.ClusterClient
	config *ClusterConfig
}

// SingleOption 单机客户端配置选项函数类型
type SingleOption func(*Single)

// ClusterOption 集群客户端配置选项函数类型
type ClusterOption func(*Cluster)

// NewClient 创建新的单机 Redis 客户端
func NewClient(config *SingleConfig, opts ...SingleOption) (*Single, error) {
	s := &Single{
		config: config,
	}

	// 初始化配置
	if err := s.config.Init(); err != nil {
		return nil, err
	}

	// 应用选项
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	// 创建客户端
	client, err := s.createClient()
	if err != nil {
		return nil, err
	}
	s.Client = client

	// 测试连接
	if err := s.Ping(context.Background()); err != nil {
		// 如果连接失败，确保清理资源
		_ = s.Close()
		return nil, err
	}

	return s, nil
}

// createClient 创建单机 Redis 客户端
func (s *Single) createClient() (*redis.Client, error) {
	poolSize := s.config.GetPoolSize()
	if poolSize == 0 {
		poolSize = 10 * runtime.GOMAXPROCS(0)
	}

	client := redis.NewClient(&redis.Options{
		Addr:            s.config.Addr(),
		Password:        s.config.GetPassword(),
		DB:              s.config.DB,
		Protocol:        s.config.GetProtocol(),
		PoolSize:        poolSize,
		DialTimeout:     time.Duration(s.config.DialTimeout) * time.Second,
		ReadTimeout:     time.Duration(s.config.ReadTimeout) * time.Second,
		WriteTimeout:    time.Duration(s.config.WriteTimeout) * time.Second,
		MaxRetries:      s.config.MaxRetries,
		MinRetryBackoff: time.Duration(s.config.MinRetryBackoff) * time.Millisecond,
		MaxRetryBackoff: time.Duration(s.config.MaxRetryBackoff) * time.Millisecond,
		PoolTimeout:     time.Duration(s.config.PoolTimeout) * time.Second,
		ConnMaxIdleTime: time.Duration(s.config.IdleTimeout) * time.Second,
		ConnMaxLifetime: time.Duration(s.config.MaxConnAge) * time.Second,
		MinIdleConns:    s.config.MinIdleConns,
	})

	return client, nil
}

// Ping 测试单机 Redis 连接是否正常
func (s *Single) Ping(ctx context.Context) error {
	if s.Client == nil {
		return ErrClientNotInitialized
	}

	_, err := s.Client.Ping(ctx).Result()
	return err
}

// Close 关闭单机 Redis 连接
func (s *Single) Close() error {
	if s.Client == nil {
		return nil
	}

	err := s.Client.Close()
	s.Client = nil // 清空引用，避免重复关闭
	return err
}

// Stats 获取单机 Redis 连接池统计信息
func (s *Single) Stats() *redis.PoolStats {
	if s.Client == nil {
		return nil
	}

	return s.Client.PoolStats()
}

// GetClient 获取单机 Redis 客户端实例
func (s *Single) GetClient() *redis.Client {
	return s.Client
}

// NewClusterClient 创建新的集群 Redis 客户端
func NewClusterClient(config *ClusterConfig, opts ...ClusterOption) (*Cluster, error) {
	cl := &Cluster{
		config: config,
	}

	// 初始化配置
	if err := cl.config.Init(); err != nil {
		return nil, err
	}

	// 应用选项
	for _, opt := range opts {
		if opt != nil {
			opt(cl)
		}
	}

	// 创建客户端
	client, err := cl.createClusterClient()
	if err != nil {
		return nil, err
	}
	cl.Client = client

	// 测试连接
	if err := cl.Ping(context.Background()); err != nil {
		// 如果连接失败，确保清理资源
		_ = cl.Close()
		return nil, err
	}

	return cl, nil
}

// createClusterClient 创建集群 Redis 客户端
func (cl *Cluster) createClusterClient() (*redis.ClusterClient, error) {
	poolSize := cl.config.GetPoolSize()
	if poolSize == 0 {
		poolSize = 10 * runtime.GOMAXPROCS(0)
	}

	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    cl.config.Addrs,
		Password: cl.config.GetPassword(),
		Protocol: cl.config.GetProtocol(),
		PoolSize: poolSize,
	})

	return client, nil
}

// Ping 测试集群 Redis 连接是否正常
func (cl *Cluster) Ping(ctx context.Context) error {
	if cl.Client == nil {
		return ErrClientNotInitialized
	}

	_, err := cl.Client.Ping(ctx).Result()
	return err
}

// Close 关闭集群 Redis 连接
func (cl *Cluster) Close() error {
	if cl.Client == nil {
		return nil
	}

	err := cl.Client.Close()
	cl.Client = nil // 清空引用，避免重复关闭
	return err
}

// Stats 获取集群 Redis 连接池统计信息
func (cl *Cluster) Stats() *redis.PoolStats {
	if cl.Client == nil {
		return nil
	}

	return cl.Client.PoolStats()
}

// GetClient 获取集群 Redis 客户端实例
func (cl *Cluster) GetClient() *redis.ClusterClient {
	return cl.Client
}
