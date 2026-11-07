package scheduler

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/kochabx/kit/core/defaults"
	kitvalidator "github.com/kochabx/kit/core/validator"
	"github.com/kochabx/kit/log"
)

type Role uint8

const (
	RoleDispatcher Role = 1 << iota
	RoleWorker
	RoleCombined = RoleDispatcher | RoleWorker
)

var (
	validNamespace  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	configValidator = kitvalidator.Validate
)

type WorkerConfig struct {
	Queues            []QueueConfig       `json:"queues" validate:"required,dive"`
	ReplayInterval    time.Duration       `json:"replay_interval" validate:"gte=0"`
	Namespace         string              `json:"namespace" default:"scheduler" validate:"required,max=128,ascii"`
	Role              Role                `json:"role" default:"3" validate:"oneof=1 2 3"`
	MaxPayloadBytes   int                 `json:"max_payload_bytes" default:"1048576" validate:"gt=0"`
	OperationTimeout  time.Duration       `json:"operation_timeout" default:"5s" validate:"gt=0"`
	FailureBackoff    time.Duration       `json:"failure_backoff" default:"100ms" validate:"gt=0"`
	FailureBackoffMax time.Duration       `json:"failure_backoff_max" default:"10s" validate:"gtefield=FailureBackoff"`
	Execution         ExecutionConfig     `json:"execution"`
	Dispatch          DispatchConfig      `json:"dispatch"`
	Maintenance       MaintenanceConfig   `json:"maintenance"`
	Retention         RetentionConfig     `json:"retention"`
	History           HistoryConfig       `json:"history"`
	Observability     ObservabilityConfig `json:"observability"`
}

// QueueConfig reserves an independent worker pool and intake buffer.
type QueueConfig struct {
	Name         string `json:"name" validate:"required,max=128,ascii"`
	Concurrency  int    `json:"concurrency" default:"16" validate:"gt=0"`
	FetchBatch   int64  `json:"fetch_batch" default:"16" validate:"gt=0"`
	ReadMaxBytes int64  `json:"read_max_bytes" default:"1048576" validate:"gt=0"`
	Prefetch     int    `json:"prefetch" validate:"gte=0"`
}

type ExecutionConfig struct {
	// Nil subscribes to all declared queues; an empty slice subscribes to none.
	Queues                    []string      `json:"queues" validate:"dive,required"`
	PollTimeout               time.Duration `json:"poll_timeout" default:"2s" validate:"gt=0"`
	LeaseDuration             time.Duration `json:"lease_duration" default:"30s" validate:"gte=3000000000"`
	LeaseRenewInterval        time.Duration `json:"lease_renew_interval" default:"1s" validate:"gt=0,ltfield=LeaseDuration"`
	CancellationCheckInterval time.Duration `json:"cancellation_check_interval" default:"500ms" validate:"gt=0,ltfield=LeaseDuration"`
	ShutdownTimeout           time.Duration `json:"shutdown_timeout" default:"30s" validate:"gt=0"`
	RecoveryBatch             int64         `json:"recovery_batch" default:"100" validate:"gt=0"`
	RecoveryLimit             int64         `json:"recovery_limit" default:"1000" validate:"gt=0"`
}

type DispatchConfig struct {
	// Nil dispatches all declared queues; an empty slice dispatches none.
	Queues         []string      `json:"queues" validate:"dive,required"`
	CronCacheSize  int           `json:"cron_cache_size" default:"1024" validate:"gt=0"`
	Batch          int64         `json:"batch" default:"100" validate:"gt=0"`
	DrainLimit     int64         `json:"drain_limit" default:"2000" validate:"gtefield=Batch"`
	CronDrainLimit int64         `json:"cron_drain_limit" default:"2000" validate:"gtefield=Batch"`
	Interval       time.Duration `json:"interval" default:"250ms" validate:"gt=0"`
}

type MaintenanceConfig struct {
	Interval            time.Duration `json:"interval" default:"1m" validate:"gt=0"`
	Batch               int64         `json:"batch" default:"500" validate:"gt=0"`
	DrainLimit          int64         `json:"drain_limit" default:"5000" validate:"gtefield=Batch"`
	LeaseDuration       time.Duration `json:"lease_duration" default:"30s" validate:"gt=0,ltefield=Interval"`
	ConsumerIdleTimeout time.Duration `json:"consumer_idle_timeout" default:"5m" validate:"gt=0"`
}

type RetentionConfig struct {
	Submission time.Duration `json:"submission" default:"24h" validate:"gt=0"`
	Completion time.Duration `json:"completion" default:"1h" validate:"gt=0"`
	Succeeded  time.Duration `json:"succeeded" validate:"gte=0"`
	Cancelled  time.Duration `json:"cancelled" default:"1h" validate:"gte=0"`
	Expired    time.Duration `json:"expired" default:"1h" validate:"gte=0"`
	Dead       time.Duration `json:"dead" default:"72h" validate:"gt=0"`
	Schedule   time.Duration `json:"schedule" default:"168h" validate:"gt=0"`
}

type ObservabilityConfig struct {
	ObserverTimeout time.Duration `json:"observer_timeout" default:"5s" validate:"gt=0"`
	Buffer          int           `json:"buffer" default:"1024" validate:"gt=0"`
	Logger          *log.Logger   `json:"-" validate:"-"`
	Observer        Observer      `json:"-" validate:"-"`
}

func prepareConfig(config WorkerConfig) (WorkerConfig, error) {
	config.Queues = slices.Clone(config.Queues)
	config.Execution.Queues = slices.Clone(config.Execution.Queues)
	config.Dispatch.Queues = slices.Clone(config.Dispatch.Queues)
	if len(config.Queues) == 0 {
		config.Queues = []QueueConfig{{Name: DefaultQueue}}
	}
	if err := defaults.Apply(&config); err != nil {
		return WorkerConfig{}, fmt.Errorf("apply scheduler defaults: %w: %w", ErrInvalidArgument, err)
	}
	if err := configValidator.Struct(context.Background(), &config); err != nil {
		return WorkerConfig{}, fmt.Errorf("validate scheduler config: %w: %w", ErrInvalidArgument, err)
	}
	// Leave enough lease headroom for batching and an operation timeout.
	if config.Execution.LeaseRenewInterval > config.Execution.LeaseDuration/2 {
		return WorkerConfig{}, fmt.Errorf("validate scheduler config: lease renew interval must not exceed half the lease duration: %w", ErrInvalidArgument)
	}
	seen := make(map[string]bool, len(config.Queues))
	for _, queue := range config.Queues {
		if !validName.MatchString(queue.Name) || seen[queue.Name] {
			return WorkerConfig{}, fmt.Errorf("invalid or duplicate queue %q: %w", queue.Name, ErrInvalidArgument)
		}
		seen[queue.Name] = true
	}
	for _, subscription := range []struct {
		label string
		names []string
	}{{"execution", config.Execution.Queues}, {"dispatch", config.Dispatch.Queues}} {
		assigned := make(map[string]bool, len(subscription.names))
		for _, name := range subscription.names {
			if !seen[name] || assigned[name] {
				return WorkerConfig{}, fmt.Errorf("invalid %s queue subscription %q: %w", subscription.label, name, ErrInvalidArgument)
			}
			assigned[name] = true
		}
	}
	if !validNamespace.MatchString(config.Namespace) {
		return WorkerConfig{}, fmt.Errorf("validate scheduler config: namespace must match %s: %w", validNamespace, ErrInvalidArgument)
	}
	if config.Observability.Logger == nil {
		config.Observability.Logger = log.Global()
	}
	return config, nil
}

// HistoryConfig enables bounded per-job attempt history. MaxEntries=0 disables it.
type HistoryConfig struct {
	MaxEntries int           `json:"max_entries" validate:"gte=0,lte=1000"`
	TTL        time.Duration `json:"ttl" default:"72h" validate:"gt=0"`
}
