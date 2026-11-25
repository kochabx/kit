package asynq

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/hibiken/asynq"
	goredis "github.com/redis/go-redis/v9"
)

const defaultShutdownTimeout = 8 * time.Second

// Config configures thin wrappers around github.com/hibiken/asynq Client, Server, and Scheduler.
type Config struct {
	RedisClient         goredis.UniversalClient `json:"-"`
	RedisOpt            asynq.RedisConnOpt      `json:"-"`
	Concurrency         int                     `json:"concurrency"`
	Queues              map[string]int          `json:"queues"`
	ShutdownTimeout     time.Duration           `json:"shutdown_timeout"`
	StrictPriority      bool                    `json:"strict_priority"`
	Location            *time.Location          `json:"-"`
	TaskCheckInterval   time.Duration           `json:"task_check_interval"`
	HealthCheckInterval time.Duration           `json:"health_check_interval"`
	EnableTracing       *bool                   `json:"enable_tracing"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if c.RedisClient == nil && c.RedisOpt == nil {
		errs = append(errs, errors.New("asynq: either RedisClient or RedisOpt is required"))
	}
	if c.Concurrency < 0 {
		errs = append(errs, errors.New("asynq: concurrency cannot be negative"))
	}
	if c.ShutdownTimeout < 0 {
		errs = append(errs, errors.New("asynq: shutdown_timeout cannot be negative"))
	}
	if c.TaskCheckInterval < 0 {
		errs = append(errs, errors.New("asynq: task_check_interval cannot be negative"))
	}
	if c.HealthCheckInterval < 0 {
		errs = append(errs, errors.New("asynq: health_check_interval cannot be negative"))
	}
	for q, prio := range c.Queues {
		if q == "" || prio <= 0 {
			errs = append(errs, fmt.Errorf("asynq: invalid queue %q weight %d", q, prio))
		}
	}
	return errors.Join(errs...)
}

func (c Config) serverConfig(logger asynq.Logger) asynq.Config {
	var queues map[string]int
	if len(c.Queues) > 0 {
		queues = maps.Clone(c.Queues)
	}
	return asynq.Config{
		Concurrency:         c.Concurrency,
		Queues:              queues,
		StrictPriority:      c.StrictPriority,
		ShutdownTimeout:     cmp.Or(c.ShutdownTimeout, defaultShutdownTimeout),
		TaskCheckInterval:   c.TaskCheckInterval,
		HealthCheckInterval: c.HealthCheckInterval,
		Logger:              logger,
	}
}
