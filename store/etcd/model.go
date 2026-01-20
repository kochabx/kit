package etcd

import (
	"time"

	"github.com/kochabx/kit/core/tag"
)

// Config ETCD 配置
type Config struct {
	Endpoints           []string      `json:"endpoints" default:"localhost:2379"`
	Username            string        `json:"username" default:"root"`
	Password            string        `json:"password"`
	DialTimeout         time.Duration `json:"dialTimeout" default:"5s"`
	KeepAliveTime       time.Duration `json:"keepAliveTime" default:"30s"`
	KeepAliveTimeout    time.Duration `json:"keepAliveTimeout" default:"5s"`
	AutoSyncInterval    time.Duration `json:"autoSyncInterval" default:"0s"`
	MaxSendMsgSize      int           `json:"maxSendMsgSize" default:"2097152"` // 2MB
	MaxRecvMsgSize      int           `json:"maxRecvMsgSize" default:"4194304"` // 4MB
	RejectOldCluster    bool          `json:"rejectOldCluster" default:"false"`
	PermitWithoutStream bool          `json:"permitWithoutStream" default:"false"`
}

func (c *Config) init() error {
	return tag.ApplyDefaults(c)
}
