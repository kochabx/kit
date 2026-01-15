package redis

import (
	"strconv"
	"strings"

	"github.com/kochabx/kit/core/tag"
)

// ClientType Redis 客户端类型
type ClientType string

const (
	ClientSingle  ClientType = "single"
	ClientCluster ClientType = "cluster"
)

// RedisConfig Redis 配置接口
// 用于不同 Redis 客户端类型的配置实现
type RedisConfig interface {
	ClientType() ClientType
	Init() error
	GetPoolSize() int
	GetPassword() string
	GetProtocol() int
}

// SingleConfig 单机 Redis 配置
type SingleConfig struct {
	Host     string `json:"host" default:"localhost"`
	Port     int    `json:"port" default:"6379"`
	Password string `json:"password"`
	DB       int    `json:"db" default:"0"`
	Protocol int    `json:"protocol" default:"3"`
	PoolSize int    `json:"poolSize" default:"0"` // 0 表示使用默认值

	// 连接超时配置
	DialTimeout  int `json:"dialTimeout" default:"5"`  // 连接超时（秒）
	ReadTimeout  int `json:"readTimeout" default:"3"`  // 读超时（秒）
	WriteTimeout int `json:"writeTimeout" default:"3"` // 写超时（秒）

	// 连接池配置
	MaxRetries      int `json:"maxRetries" default:"3"`        // 最大重试次数
	MinRetryBackoff int `json:"minRetryBackoff" default:"8"`   // 最小重试间隔（毫秒）
	MaxRetryBackoff int `json:"maxRetryBackoff" default:"512"` // 最大重试间隔（毫秒）

	// 连接池大小配置
	PoolTimeout   int `json:"poolTimeout" default:"4"`    // 连接池超时（秒）
	IdleTimeout   int `json:"idleTimeout" default:"300"`  // 空闲连接超时（秒）
	IdleCheckFreq int `json:"idleCheckFreq" default:"60"` // 空闲检查频率（秒）
	MaxConnAge    int `json:"maxConnAge" default:"0"`     // 连接最大生存时间（秒），0表示不限制
	MinIdleConns  int `json:"minIdleConns" default:"0"`   // 最小空闲连接数
}

func (c *SingleConfig) ClientType() ClientType {
	return ClientSingle
}

func (c *SingleConfig) Init() error {
	return tag.ApplyDefaults(c)
}

func (c *SingleConfig) GetPoolSize() int {
	return c.PoolSize
}

func (c *SingleConfig) GetPassword() string {
	return c.Password
}

func (c *SingleConfig) GetProtocol() int {
	return c.Protocol
}

func (c *SingleConfig) Addr() string {
	var builder strings.Builder
	builder.WriteString(c.Host)
	builder.WriteString(":")
	builder.WriteString(strconv.Itoa(c.Port))
	return builder.String()
}

// ClusterConfig 集群 Redis 配置
type ClusterConfig struct {
	Addrs    []string `json:"addrs" default:"localhost:6379"`
	Password string   `json:"password"`
	Protocol int      `json:"protocol" default:"3"`
	PoolSize int      `json:"poolSize" default:"0"` // 0 表示使用默认值

	// 连接超时配置
	DialTimeout  int `json:"dialTimeout" default:"5"`  // 连接超时（秒）
	ReadTimeout  int `json:"readTimeout" default:"3"`  // 读超时（秒）
	WriteTimeout int `json:"writeTimeout" default:"3"` // 写超时（秒）

	// 连接池配置
	MaxRetries      int `json:"maxRetries" default:"3"`        // 最大重试次数
	MinRetryBackoff int `json:"minRetryBackoff" default:"8"`   // 最小重试间隔（毫秒）
	MaxRetryBackoff int `json:"maxRetryBackoff" default:"512"` // 最大重试间隔（毫秒）

	// 连接池大小配置
	PoolTimeout   int `json:"poolTimeout" default:"4"`    // 连接池超时（秒）
	IdleTimeout   int `json:"idleTimeout" default:"300"`  // 空闲连接超时（秒）
	IdleCheckFreq int `json:"idleCheckFreq" default:"60"` // 空闲检查频率（秒）
	MaxConnAge    int `json:"maxConnAge" default:"0"`     // 连接最大生存时间（秒），0表示不限制
	MinIdleConns  int `json:"minIdleConns" default:"0"`   // 最小空闲连接数

	// 集群特有配置
	MaxRedirects   int  `json:"maxRedirects" default:"3"`       // 最大重定向次数
	ReadOnly       bool `json:"readOnly" default:"false"`       // 是否只读
	RouteByLatency bool `json:"routeByLatency" default:"false"` // 是否按延迟路由
	RouteRandomly  bool `json:"routeRandomly" default:"false"`  // 是否随机路由
}

func (c *ClusterConfig) ClientType() ClientType {
	return ClientCluster
}

func (c *ClusterConfig) Init() error {
	return tag.ApplyDefaults(c)
}

func (c *ClusterConfig) GetPoolSize() int {
	return c.PoolSize
}

func (c *ClusterConfig) GetPassword() string {
	return c.Password
}

func (c *ClusterConfig) GetProtocol() int {
	return c.Protocol
}
