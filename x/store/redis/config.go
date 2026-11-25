package redis

import (
	"crypto/tls"
	"errors"
	"fmt"
	"time"
)

const (
	defaultDialTimeout        = 5 * time.Second
	defaultReadTimeout        = 3 * time.Second
	defaultWriteTimeout       = 3 * time.Second
	defaultPoolTimeout        = 4 * time.Second
	defaultSlowQueryThreshold = 100 * time.Millisecond
)

// Config configures a standalone, sentinel failover, or cluster Redis client.
type Config struct {
	Addrs              []string      `json:"addrs"`
	MasterName         string        `json:"master_name"`
	Cluster            bool          `json:"cluster"`
	ClientName         string        `json:"client_name"`
	Username           string        `json:"username"`
	Password           string        `json:"password"`
	SentinelUsername   string        `json:"sentinel_username"`
	SentinelPassword   string        `json:"sentinel_password"`
	DB                 int           `json:"db"`
	PoolSize           int           `json:"pool_size"`
	MinIdleConns       int           `json:"min_idle_conns"`
	DialTimeout        time.Duration `json:"dial_timeout"`
	ReadTimeout        time.Duration `json:"read_timeout"`
	WriteTimeout       time.Duration `json:"write_timeout"`
	PoolTimeout        time.Duration `json:"pool_timeout"`
	MaxRetries         int           `json:"max_retries"`
	TLS                *tls.Config   `json:"-"`
	SlowQueryThreshold time.Duration `json:"slow_query_threshold"`
	EnableTracing      *bool         `json:"enable_tracing"`
	EnableMetrics      *bool         `json:"enable_metrics"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if len(c.Addrs) == 0 {
		errs = append(errs, errors.New("redis: at least one address is required"))
	}
	for i, addr := range c.Addrs {
		if addr == "" {
			errs = append(errs, fmt.Errorf("redis: addrs[%d] cannot be empty", i))
		}
	}
	if c.Cluster && c.MasterName != "" {
		errs = append(errs, errors.New("redis: cluster mode and master_name (sentinel) are mutually exclusive"))
	}
	if c.Cluster && c.DB > 0 {
		errs = append(errs, errors.New("redis: db must be 0 in cluster mode"))
	}
	if c.DB < 0 {
		errs = append(errs, errors.New("redis: db cannot be negative"))
	}
	if c.PoolSize < 0 {
		errs = append(errs, errors.New("redis: pool_size cannot be negative"))
	}
	if c.MinIdleConns < 0 {
		errs = append(errs, errors.New("redis: min_idle_conns cannot be negative"))
	}
	if c.PoolSize > 0 && c.MinIdleConns > c.PoolSize {
		errs = append(errs, errors.New("redis: min_idle_conns cannot exceed pool_size"))
	}
	if c.DialTimeout < 0 {
		errs = append(errs, errors.New("redis: dial_timeout cannot be negative"))
	}
	if c.ReadTimeout < 0 {
		errs = append(errs, errors.New("redis: read_timeout cannot be negative"))
	}
	if c.WriteTimeout < 0 {
		errs = append(errs, errors.New("redis: write_timeout cannot be negative"))
	}
	if c.PoolTimeout < 0 {
		errs = append(errs, errors.New("redis: pool_timeout cannot be negative"))
	}
	if c.MaxRetries < -1 {
		errs = append(errs, errors.New("redis: max_retries cannot be less than -1"))
	}
	if c.SlowQueryThreshold < 0 {
		errs = append(errs, errors.New("redis: slow_query_threshold cannot be negative"))
	}
	return errors.Join(errs...)
}
