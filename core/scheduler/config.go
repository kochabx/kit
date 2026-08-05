package scheduler

import (
	"context"
	"fmt"
	"regexp"
	"time"

	playground "github.com/go-playground/validator/v10"
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
	configValidator = newConfigValidator()
)

type Config struct {
	Namespace                 string        `json:"namespace" default:"scheduler" validate:"scheduler_namespace"`
	Concurrency               int           `json:"concurrency" default:"16" validate:"gt=0"`
	Role                      Role          `json:"role" default:"3" validate:"oneof=1 2 3"`
	DispatchBatch             int64         `json:"dispatch_batch" default:"100" validate:"gt=0"`
	DispatchDrainLimit        int64         `json:"dispatch_drain_limit" default:"2000" validate:"gtefield=DispatchBatch"`
	CronDrainLimit            int64         `json:"cron_drain_limit" default:"2000" validate:"gtefield=DispatchBatch"`
	DispatchInterval          time.Duration `json:"dispatch_interval" default:"250ms" validate:"gt=0"`
	PollTimeout               time.Duration `json:"poll_timeout" default:"2s" validate:"gt=0"`
	LeaseDuration             time.Duration `json:"lease_duration" default:"30s" validate:"gte=3000000000"`
	LeaseRenewInterval        time.Duration `json:"lease_renew_interval" default:"1s" validate:"gt=0,ltfield=LeaseDuration"`
	CancellationCheckInterval time.Duration `json:"cancellation_check_interval" default:"500ms" validate:"gt=0,ltfield=LeaseDuration"`
	ShutdownTimeout           time.Duration `json:"shutdown_timeout" default:"30s" validate:"gt=0"`
	Retention                 time.Duration `json:"retention" default:"24h" validate:"gt=0"`
	DeadRetention             time.Duration `json:"dead_retention" default:"168h" validate:"gt=0"`
	MaxPayloadBytes           int           `json:"max_payload_bytes" default:"1048576" validate:"gt=0"`
	OperationTimeout          time.Duration `json:"operation_timeout" default:"5s" validate:"gt=0"`
	FetchBatch                int64         `json:"fetch_batch" default:"16" validate:"gt=0"`
	Prefetch                  int           `json:"prefetch" default:"16" validate:"gte=0"`
	RecoveryBatch             int64         `json:"recovery_batch" default:"100" validate:"gt=0"`
	RecoveryLimit             int64         `json:"recovery_limit" default:"1000" validate:"gt=0"`
	FailureBackoff            time.Duration `json:"failure_backoff" default:"100ms" validate:"gt=0"`
	FailureBackoffMax         time.Duration `json:"failure_backoff_max" default:"10s" validate:"gtefield=FailureBackoff"`
	MaintenanceInterval       time.Duration `json:"maintenance_interval" default:"1m" validate:"gt=0"`
	MaintenanceBatch          int64         `json:"maintenance_batch" default:"500" validate:"gt=0"`
	MaintenanceDrainLimit     int64         `json:"maintenance_drain_limit" default:"5000" validate:"gtefield=MaintenanceBatch"`
	MaintenanceLeaseDuration  time.Duration `json:"maintenance_lease_duration" default:"30s" validate:"gt=0,ltefield=MaintenanceInterval"`
	ConsumerIdleTimeout       time.Duration `json:"consumer_idle_timeout" default:"5m" validate:"gt=0"`
	ObserverBuffer            int           `json:"observer_buffer" default:"1024" validate:"gt=0"`
	ReplayInterval            time.Duration `json:"replay_interval" validate:"gte=0"`
	Logger                    *log.Logger   `json:"-" validate:"-"`
	Observer                  Observer      `json:"-" validate:"-"`
}

func prepareConfig(config Config) (Config, error) {
	if err := defaults.Apply(&config); err != nil {
		return Config{}, fmt.Errorf("apply scheduler defaults: %w", err)
	}
	if err := configValidator.Struct(context.Background(), &config); err != nil {
		return Config{}, fmt.Errorf("validate scheduler config: %w", err)
	}
	if config.Logger == nil {
		config.Logger = log.Global()
	}
	if config.Observer == nil {
		config.Observer = noopObserver{}
	}
	return config, nil
}

func newConfigValidator() kitvalidator.Validator {
	v, err := kitvalidator.New(kitvalidator.WithValidation(
		"scheduler_namespace",
		func(field playground.FieldLevel) bool {
			return validNamespace.MatchString(field.Field().String())
		},
	))
	if err != nil {
		panic(fmt.Sprintf("create scheduler config validator: %v", err))
	}
	return v
}
