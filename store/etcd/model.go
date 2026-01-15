package etcd

import "github.com/kochabx/kit/core/tag"

// Config ETCD 配置
type Config struct {
	Endpoints           []string `json:"endpoints" default:"localhost:2379"`
	Username            string   `json:"username" default:"root"`
	Password            string   `json:"password"`
	DialTimeout         int64    `json:"dialTimeout" default:"5"`
	KeepAliveTime       int64    `json:"keepAliveTime" default:"30"`
	KeepAliveTimeout    int64    `json:"keepAliveTimeout" default:"5"`
	AutoSyncInterval    int64    `json:"autoSyncInterval" default:"0"`
	MaxSendMsgSize      int      `json:"maxSendMsgSize" default:"2097152"` // 2MB
	MaxRecvMsgSize      int      `json:"maxRecvMsgSize" default:"4194304"` // 4MB
	RejectOldCluster    bool     `json:"rejectOldCluster" default:"false"`
	PermitWithoutStream bool     `json:"permitWithoutStream" default:"false"`
}

func (c *Config) init() error {
	return tag.ApplyDefaults(c)
}
