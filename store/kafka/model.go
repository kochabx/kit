package kafka

import (
	"github.com/segmentio/kafka-go"

	"github.com/kochabx/kit/core/stag"
)

type Balancer int

const (
	BalancerLeastBytes Balancer = iota
	BalancerHash
)

type Config struct {
	Brokers                []string `json:"brokers" default:"localhost:9092"`
	Username               string   `json:"username"`
	Password               string   `json:"password"`
	Balancer               Balancer `json:"balancer" default:"0"`
	Partition              int      `json:"partition" default:"0"`
	AllowAutoTopicCreation bool     `json:"allowAutoTopicCreation" default:"false"`
	Timeout                int      `json:"timeout" default:"3"`
	MinBytes               float64  `json:"minBytes" default:"1024"`    // 1KB
	MaxBytes               float64  `json:"maxBytes" default:"1048576"` // 1MB
	CloseTimeout           int      `json:"closeTimeout" default:"5"`
}

func (c *Config) init() error {
	return stag.ApplyDefaults(c)
}

func (c *Config) balancer() kafka.Balancer {
	switch c.Balancer {
	case BalancerLeastBytes:
		return &kafka.LeastBytes{}
	case BalancerHash:
		return &kafka.Hash{}
	default:
		return &kafka.LeastBytes{}
	}
}
