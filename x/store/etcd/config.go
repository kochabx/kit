package etcd

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultDialTimeout      = 5 * time.Second
	defaultOperationTimeout = 5 * time.Second
	defaultLockTTLSeconds   = 30
)

// Config configures an etcd v3 Client.
type Config struct {
	Endpoints            []string      `json:"endpoints"`
	Username             string        `json:"username"`
	Password             string        `json:"password"`
	DialTimeout          time.Duration `json:"dial_timeout"`
	DialKeepAliveTime    time.Duration `json:"dial_keep_alive_time"`
	DialKeepAliveTimeout time.Duration `json:"dial_keep_alive_timeout"`
	AutoSyncInterval     time.Duration `json:"auto_sync_interval"`
	RejectOldCluster     bool          `json:"reject_old_cluster"`
	PermitWithoutStream  bool          `json:"permit_without_stream"`
	TLS                  *tls.Config   `json:"-"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if len(c.Endpoints) == 0 {
		errs = append(errs, errors.New("etcd: at least one endpoint is required"))
	}
	for i, ep := range c.Endpoints {
		if strings.TrimSpace(ep) == "" {
			errs = append(errs, fmt.Errorf("etcd: endpoints[%d] cannot be empty", i))
		}
	}
	if c.DialTimeout < 0 {
		errs = append(errs, errors.New("etcd: dial_timeout cannot be negative"))
	}
	if c.DialKeepAliveTime < 0 {
		errs = append(errs, errors.New("etcd: dial_keep_alive_time cannot be negative"))
	}
	if c.DialKeepAliveTimeout < 0 {
		errs = append(errs, errors.New("etcd: dial_keep_alive_timeout cannot be negative"))
	}
	if c.AutoSyncInterval < 0 {
		errs = append(errs, errors.New("etcd: auto_sync_interval cannot be negative"))
	}
	return errors.Join(errs...)
}
