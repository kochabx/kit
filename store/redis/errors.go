package redis

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

// 错误定义
var (
	// ErrNil redis.Nil 的封装，表示 key 不存在
	ErrNil = redis.Nil

	// ErrClientClosed 客户端已关闭
	ErrClientClosed = errors.New("redis: client is closed")

	// ErrClientNotInitialized 客户端未初始化
	ErrClientNotInitialized = errors.New("redis: client not initialized")

	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = errors.New("redis: invalid configuration")

	// ErrEmptyAddrs 地址列表为空
	ErrEmptyAddrs = errors.New("redis: addrs cannot be empty")

	// ErrInvalidTimeout 超时配置无效
	ErrInvalidTimeout = errors.New("redis: invalid timeout value")

	// ErrHealthCheckFailed 健康检查失败
	ErrHealthCheckFailed = errors.New("redis: health check failed")

	// ErrSentinelMasterNameRequired 哨兵模式需要 MasterName
	ErrSentinelMasterNameRequired = errors.New("redis: sentinel mode requires master name")
)
