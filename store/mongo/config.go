package mongo

import (
	"context"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
)

// Config contains structured MongoDB connection and pool settings.
type Config struct {
	Hosts      []string `json:"hosts" default:"[\"localhost:27017\"]" validate:"required,min=1,dive,required"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	AuthSource string   `json:"authSource"`

	ReplicaSet string `json:"replicaSet"`
	Direct     bool   `json:"direct"`

	MaxPoolSize uint64 `json:"maxPoolSize" default:"10" validate:"gte=1"`
	MinPoolSize uint64 `json:"minPoolSize"`

	ConnectTimeout         time.Duration `json:"connectTimeout" default:"3s" validate:"gt=0"`
	ServerSelectionTimeout time.Duration `json:"serverSelectionTimeout" default:"3s" validate:"gt=0"`
}

func resolveConfig(cfg Config) (Config, error) {
	if err := defaults.Apply(&cfg); err != nil {
		return Config{}, fmt.Errorf("%w: apply defaults: %w", ErrInvalidConfig, err)
	}
	if err := validator.Validate.Struct(context.Background(), &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: validate config: %w", ErrInvalidConfig, err)
	}
	if cfg.MinPoolSize > cfg.MaxPoolSize {
		return Config{}, fmt.Errorf("%w: minimum pool size exceeds maximum pool size", ErrInvalidConfig)
	}
	if cfg.Direct && len(cfg.Hosts) != 1 {
		return Config{}, fmt.Errorf("%w: direct connections require exactly one host", ErrInvalidConfig)
	}
	return cfg, nil
}
