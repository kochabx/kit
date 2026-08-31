package etcd

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
)

// Config contains etcd connection settings.
type Config struct {
	Endpoints []string `json:"endpoints" default:"[\"localhost:2379\"]" validate:"required,min=1,dive,required"`
	Username  string   `json:"username"`
	Password  string   `json:"password"`

	DialTimeout         time.Duration `json:"dialTimeout" default:"5s" validate:"gt=0"`
	KeepAliveTime       time.Duration `json:"keepAliveTime" default:"30s" validate:"gt=0"`
	KeepAliveTimeout    time.Duration `json:"keepAliveTimeout" default:"5s" validate:"gt=0"`
	AutoSyncInterval    time.Duration `json:"autoSyncInterval" validate:"gte=0"`
	MaxSendMsgSize      int           `json:"maxSendMsgSize" validate:"gte=0"`
	MaxRecvMsgSize      int           `json:"maxRecvMsgSize" validate:"gte=0"`
	RejectOldCluster    bool          `json:"rejectOldCluster"`
	PermitWithoutStream bool          `json:"permitWithoutStream"`

	TLS *tls.Config `json:"-" validate:"-"`
}

func resolveConfig(cfg Config) (Config, error) {
	cfg.Endpoints = append([]string(nil), cfg.Endpoints...)
	if err := defaults.Apply(&cfg); err != nil {
		return Config{}, fmt.Errorf("%w: apply defaults: %w", ErrInvalidConfig, err)
	}
	if err := validator.Validate.Struct(context.Background(), &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: validate config: %w", ErrInvalidConfig, err)
	}
	if cfg.TLS != nil {
		cfg.TLS = cfg.TLS.Clone()
	}
	return cfg, nil
}
