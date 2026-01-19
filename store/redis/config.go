package redis

import (
	"crypto/tls"

	"github.com/kochabx/kit/core/tag"
)

// Config Redis 统一配置（支持单机/集群/哨兵模式）
type Config struct {
	// ==================== 连接配置 ====================
	// Addrs Redis 地址列表
	// 单机模式: ["localhost:6379"]
	// 集群模式: ["node1:6379", "node2:6379", "node3:6379"]
	// 哨兵模式: ["sentinel1:26379", "sentinel2:26379"]
	Addrs []string

	// MasterName 哨兵模式的主节点名称
	// 仅在哨兵模式下需要设置
	MasterName string

	// ==================== 认证配置 ====================
	// Username Redis 用户名 (Redis 6.0+)
	Username string

	// Password Redis 密码
	Password string

	// DB 数据库索引（0-15）
	// 仅在单机和哨兵模式下有效，集群模式忽略此字段
	DB int

	// ==================== 协议配置 ====================
	// Protocol Redis 协议版本
	// 2: RESP2 (默认)
	// 3: RESP3 (Redis 6.0+)
	Protocol int `default:"3"`

	// ==================== 超时配置 ====================
	// DialTimeout 连接超时时间（毫秒）
	DialTimeout int64 `default:"5000"`

	// ReadTimeout 读操作超时时间（毫秒）
	ReadTimeout int64 `default:"3000"`

	// WriteTimeout 写操作超时时间（毫秒）
	WriteTimeout int64 `default:"3000"`

	// ==================== 连接池配置 ====================
	// PoolSize 连接池最大连接数
	// 0 表示使用默认值: 10 * runtime.GOMAXPROCS
	PoolSize int

	// MinIdleConns 最小空闲连接数
	MinIdleConns int

	// MaxIdleTime 空闲连接最大存活时间（毫秒）
	// 超过此时间的空闲连接将被关闭
	MaxIdleTime int64 `default:"300000"`

	// MaxLifetime 连接最大生存时间（毫秒）
	// 0 表示连接可以永久重用
	MaxLifetime int64

	// PoolTimeout 从连接池获取连接的超时时间（毫秒）
	PoolTimeout int64 `default:"4000"`

	// ==================== 重试配置 ====================
	// MaxRetries 命令失败后的最大重试次数
	// -1: 禁用重试
	//  0: 默认重试 3 次
	// >0: 指定重试次数
	MaxRetries int

	// MinRetryBackoff 最小重试退避时间（毫秒）
	MinRetryBackoff int64 `default:"8"`

	// MaxRetryBackoff 最大重试退避时间（毫秒）
	MaxRetryBackoff int64 `default:"512"`

	// ==================== TLS 配置 ====================
	// TLSConfig TLS 配置
	// 设置此字段后将使用 TLS 加密连接
	TLSConfig *tls.Config

	// ==================== 集群特有配置 ====================
	// MaxRedirects 集群模式下的最大重定向次数
	MaxRedirects int `default:"3"`

	// ReadOnly 是否启用只读模式
	// 启用后读操作会路由到从节点
	ReadOnly bool

	// RouteByLatency 是否按延迟路由
	// 启用后会选择延迟最低的节点
	RouteByLatency bool

	// RouteRandomly 是否随机路由
	// 启用后会随机选择节点
	RouteRandomly bool
}

// ApplyDefaults 应用默认值
func (c *Config) ApplyDefaults() error {
	return tag.ApplyDefaults(c)
}

// Single 创建单机模式配置
func Single(addr string) *Config {
	return &Config{Addrs: []string{addr}}
}

// Cluster 创建集群模式配置
func Cluster(addrs ...string) *Config {
	return &Config{Addrs: addrs}
}

// Sentinel 创建哨兵模式配置
func Sentinel(masterName string, addrs ...string) *Config {
	return &Config{Addrs: addrs, MasterName: masterName}
}

// Validate 验证配置是否有效
func (c *Config) Validate() error {
	if len(c.Addrs) == 0 {
		return ErrEmptyAddrs
	}

	// 验证超时配置
	if c.DialTimeout < 0 || c.ReadTimeout < 0 || c.WriteTimeout < 0 {
		return ErrInvalidTimeout
	}

	return nil
}

// IsSentinel 判断是否为哨兵模式
func (c *Config) IsSentinel() bool {
	return c.MasterName != ""
}

// IsCluster 判断是否为集群模式
func (c *Config) IsCluster() bool {
	return len(c.Addrs) > 1 && c.MasterName == ""
}

// IsSingle 判断是否为单机模式
func (c *Config) IsSingle() bool {
	return len(c.Addrs) == 1 && c.MasterName == ""
}
