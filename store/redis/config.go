package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
)

// Mode identifies the Redis deployment topology.
type Mode string

const (
	// ModeSingle creates a standalone client and requires exactly one address.
	ModeSingle Mode = "single"
	// ModeCluster creates a cluster client, including for a single configuration endpoint.
	ModeCluster Mode = "cluster"
	// ModeSentinel creates a failover client and requires MasterName.
	ModeSentinel Mode = "sentinel"
)

// Config contains connection settings shared by standalone, cluster, and
// Sentinel clients. New resolves defaults on a copy and never mutates it.
type Config struct {
	// Mode determines the concrete client type; it is not inferred from Addrs.
	Mode Mode `json:"mode" default:"single" validate:"oneof=single cluster sentinel"`
	// Addrs contains a standalone address or cluster/Sentinel seed addresses.
	Addrs []string `json:"addrs" default:"[\"localhost:6379\"]" validate:"required,min=1,dive,required"`
	// MasterName is required only in Sentinel mode.
	MasterName string `json:"masterName"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	DB         int    `json:"db" validate:"gte=0"`
	Protocol   int    `json:"protocol" default:"3" validate:"oneof=2 3"`

	DialTimeout  time.Duration `json:"dialTimeout" default:"5s" validate:"gte=0"`
	ReadTimeout  time.Duration `json:"readTimeout" default:"3s"`
	WriteTimeout time.Duration `json:"writeTimeout" default:"3s"`

	// PoolSize delegates to the go-redis default when zero.
	PoolSize     int           `json:"poolSize" validate:"gte=0"`
	MinIdleConns int           `json:"minIdleConns" validate:"gte=0"`
	MaxIdleTime  time.Duration `json:"maxIdleTime" default:"5m" validate:"gte=0"`
	MaxLifetime  time.Duration `json:"maxLifetime" validate:"gte=0"`
	PoolTimeout  time.Duration `json:"poolTimeout" default:"4s" validate:"gte=0"`

	// MaxRetries delegates to the go-redis default when zero; -1 disables retries.
	MaxRetries      int           `json:"maxRetries" validate:"gte=-1"`
	MinRetryBackoff time.Duration `json:"minRetryBackoff" default:"8ms" validate:"gte=-1"`
	MaxRetryBackoff time.Duration `json:"maxRetryBackoff" default:"512ms" validate:"gte=-1"`

	// TLSConfig enables TLS and is intentionally excluded from JSON configuration.
	TLSConfig *tls.Config `json:"-" validate:"-"`

	MaxRedirects   int  `json:"maxRedirects" default:"3" validate:"gte=0"`
	ReadOnly       bool `json:"readOnly"`
	RouteByLatency bool `json:"routeByLatency"`
	RouteRandomly  bool `json:"routeRandomly"`
}

// Single returns a standalone Redis configuration.
func Single(addr string) *Config {
	return &Config{Mode: ModeSingle, Addrs: []string{addr}}
}

// Cluster returns a Redis Cluster configuration. A single address is valid for
// managed services that expose one cluster configuration endpoint.
func Cluster(addrs ...string) *Config {
	return &Config{Mode: ModeCluster, Addrs: addrs}
}

// Sentinel returns a Redis Sentinel configuration for masterName.
func Sentinel(masterName string, addrs ...string) *Config {
	return &Config{Mode: ModeSentinel, Addrs: addrs, MasterName: masterName}
}

func resolveConfig(cfg Config) (Config, error) {
	if (cfg.Mode == ModeCluster || cfg.Mode == ModeSentinel) && len(cfg.Addrs) == 0 {
		return Config{}, fmt.Errorf("%w: %s addresses are required", ErrInvalidConfig, cfg.Mode)
	}
	if err := defaults.Apply(&cfg); err != nil {
		return Config{}, fmt.Errorf("%w: apply defaults: %w", ErrInvalidConfig, err)
	}
	if err := validator.Validate.Struct(context.Background(), &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: validate config: %w", ErrInvalidConfig, err)
	}
	if cfg.PoolSize > 0 && cfg.MinIdleConns > cfg.PoolSize {
		return Config{}, fmt.Errorf("%w: minimum idle connections exceed pool size", ErrInvalidConfig)
	}
	if cfg.MaxRetryBackoff >= 0 && cfg.MinRetryBackoff > cfg.MaxRetryBackoff {
		return Config{}, fmt.Errorf("%w: minimum retry backoff exceeds maximum retry backoff", ErrInvalidConfig)
	}
	switch cfg.Mode {
	case ModeSingle:
		if len(cfg.Addrs) != 1 || cfg.MasterName != "" {
			return Config{}, fmt.Errorf("%w: single mode requires exactly one address and no master name", ErrInvalidConfig)
		}
	case ModeCluster:
		if cfg.MasterName != "" {
			return Config{}, fmt.Errorf("%w: cluster mode does not use a master name", ErrInvalidConfig)
		}
	case ModeSentinel:
		if cfg.MasterName == "" {
			return Config{}, fmt.Errorf("%w: sentinel master name is required", ErrInvalidConfig)
		}
	}
	return cfg, nil
}
