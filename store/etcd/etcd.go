package etcd

import (
	"context"
	"errors"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	ErrEtcdNotInitialized = errors.New("etcd client not initialized")
	ErrConnectionFailed   = errors.New("failed to connect to etcd")
)

// Etcd ETCD 客户端
type Etcd struct {
	Client *clientv3.Client
	config *Config
}

// Option Etcd 配置选项函数类型
type Option func(*Etcd)

// New 创建新的 Etcd 实例
func New(config *Config, opts ...Option) (*Etcd, error) {
	e := &Etcd{
		config: config,
	}

	// 初始化配置
	if err := e.config.init(); err != nil {
		return nil, err
	}

	// 应用选项
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}

	// 创建etcd连接
	if err := e.connect(); err != nil {
		return nil, err
	}

	// 测试连接
	if err := e.Ping(context.TODO()); err != nil {
		// 如果连接失败，确保清理资源
		_ = e.Close()
		return nil, err
	}

	return e, nil
}

// connect 创建etcd连接
func (e *Etcd) connect() error {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:            e.config.Endpoints,
		Username:             e.config.Username,
		Password:             e.config.Password,
		DialTimeout:          e.config.DialTimeout,
		DialKeepAliveTime:    e.config.KeepAliveTime,
		DialKeepAliveTimeout: e.config.KeepAliveTimeout,
		AutoSyncInterval:     e.config.AutoSyncInterval,
		MaxCallSendMsgSize:   e.config.MaxSendMsgSize,
		MaxCallRecvMsgSize:   e.config.MaxRecvMsgSize,
		RejectOldCluster:     e.config.RejectOldCluster,
		PermitWithoutStream:  e.config.PermitWithoutStream,
	})
	if err != nil {
		return ErrConnectionFailed
	}
	e.Client = client
	return nil
}

// Ping 测试etcd连接是否正常
func (e *Etcd) Ping(ctx context.Context) error {
	if e.Client == nil {
		return ErrEtcdNotInitialized
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 使用Status方法测试连接
	_, err := e.Client.Status(ctxWithTimeout, e.config.Endpoints[0])
	if err != nil {
		return err
	}

	return nil
}

// Status 获取etcd状态
func (e *Etcd) Status(ctx context.Context, endpoint string) (*clientv3.StatusResponse, error) {
	if e.Client == nil {
		return nil, ErrEtcdNotInitialized
	}

	return e.Client.Status(ctx, endpoint)
}

// GetClient 获取原始的etcd客户端
func (e *Etcd) GetClient() *clientv3.Client {
	return e.Client
}

// Close 关闭etcd连接
func (e *Etcd) Close() error {
	if e.Client == nil {
		return nil
	}

	if err := e.Client.Close(); err != nil {
		return err
	}

	e.Client = nil // 清空引用，避免重复关闭
	return nil
}
