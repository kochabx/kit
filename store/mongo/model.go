package mongo

import (
	"strconv"
	"strings"
	"time"

	"github.com/kochabx/kit/core/tag"
)

// Config MongoDB 配置结构体
type Config struct {
	Host        string        `json:"host" default:"localhost"`
	Port        int           `json:"port" default:"27017"`
	User        string        `json:"user" default:"root"`
	Password    string        `json:"password"`
	MaxPoolSize int           `json:"maxPoolSize" default:"10"`
	Timeout     time.Duration `json:"timeout" default:"3s"`
}

// uri 构建 MongoDB 连接字符串
func (c *Config) uri() string {
	var builder strings.Builder
	builder.Grow(128)

	builder.WriteString("mongodb://")
	if c.User != "" && c.Password != "" {
		builder.WriteString(c.User)
		builder.WriteString(":")
		builder.WriteString(c.Password)
		builder.WriteString("@")
	}

	builder.WriteString(c.Host)
	builder.WriteString(":")
	builder.WriteString(strconv.Itoa(c.Port))
	builder.WriteString("/")

	builder.WriteString("?maxPoolSize=")
	builder.WriteString(strconv.Itoa(c.MaxPoolSize))

	return builder.String()
}

// init 初始化配置，设置默认值
func (c *Config) Init() error {
	return tag.ApplyDefaults(c)
}
