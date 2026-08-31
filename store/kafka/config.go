package kafka

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
	segmentio "github.com/segmentio/kafka-go"
)

// Balancer identifies a producer partition-balancing strategy.
type Balancer string

const (
	BalancerLeastBytes Balancer = "least_bytes"
	BalancerHash       Balancer = "hash"
)

// SASLPlainConfig contains SASL/PLAIN credentials.
type SASLPlainConfig struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password"`
}

// Config contains settings shared by Kafka producers and consumers.
type Config struct {
	Brokers []string `json:"brokers" default:"[\"localhost:9092\"]" validate:"required,min=1,dive,required"`

	SASL *SASLPlainConfig `json:"sasl,omitempty"`
	TLS  *tls.Config      `json:"-" validate:"-"`

	DialTimeout  time.Duration          `json:"dialTimeout" default:"3s" validate:"gt=0"`
	Balancer     Balancer               `json:"balancer" default:"least_bytes" validate:"oneof=least_bytes hash"`
	RequiredAcks segmentio.RequiredAcks `json:"requiredAcks" default:"1" validate:"oneof=-1 0 1"`

	AllowAutoTopicCreation bool `json:"allowAutoTopicCreation"`
	MinBytes               int  `json:"minBytes" default:"1024" validate:"gt=0"`
	MaxBytes               int  `json:"maxBytes" default:"1048576" validate:"gt=0"`
}

func resolveConfig(cfg Config) (Config, error) {
	cfg.Brokers = append([]string(nil), cfg.Brokers...)
	if err := defaults.Apply(&cfg); err != nil {
		return Config{}, fmt.Errorf("%w: apply defaults: %w", ErrInvalidConfig, err)
	}
	if err := validator.Validate.Struct(context.Background(), &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: validate config: %w", ErrInvalidConfig, err)
	}
	if cfg.MinBytes > cfg.MaxBytes {
		return Config{}, fmt.Errorf("%w: minimum bytes exceed maximum bytes", ErrInvalidConfig)
	}
	if cfg.SASL != nil {
		sasl := *cfg.SASL
		cfg.SASL = &sasl
	}
	if cfg.TLS != nil {
		cfg.TLS = cfg.TLS.Clone()
	}
	return cfg, nil
}

func (cfg Config) newBalancer() segmentio.Balancer {
	if cfg.Balancer == BalancerHash {
		return &segmentio.Hash{}
	}
	return &segmentio.LeastBytes{}
}
