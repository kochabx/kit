package scheduler

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/kochabx/kit/core/scheduler/internal/store"

	"github.com/redis/go-redis/v9"
)

type workerRuntime struct {
	*clientState
	queues      map[string]*queueRuntime
	workers     []*queueRuntime
	dispatchers []*queueRuntime
	config      WorkerConfig
	consumerID  string

	mu        sync.RWMutex
	handlers  map[handlerKey]handler
	running   atomic.Bool
	active    sync.Map
	forceStop chan struct{}
}

func newWorkerRuntime(client redis.UniversalClient, config WorkerConfig) (*workerRuntime, error) {
	if isNilInterface(client) {
		return nil, fmt.Errorf("redis client is required: %w", ErrInvalidArgument)
	}
	config, err := prepareConfig(config)
	if err != nil {
		return nil, err
	}
	options := make(map[string]store.QueueOptions, len(config.Queues))
	names := make([]string, 0, len(config.Queues))
	for _, q := range config.Queues {
		names = append(names, q.Name)
		options[q.Name] = store.QueueOptions{
			ReadMaxBytes:        q.ReadMaxBytes,
			ClaimIdle:           config.Execution.LeaseDuration,
			DeadRetention:       config.Retention.Dead,
			SubmissionRetention: config.Retention.Submission,
			CompletionRetention: config.Retention.Completion,
			HistoryLimit:        config.History.MaxEntries,
			HistoryTTL:          config.History.TTL,
		}
	}
	state, err := newClientState(
		client,
		ClientConfig{
			Namespace:        config.Namespace,
			Queues:           names,
			MaxPayloadBytes:  config.MaxPayloadBytes,
			OperationTimeout: config.OperationTimeout,
			ReplayInterval:   config.ReplayInterval,
			Retention:        config.Retention,
			Observability:    config.Observability,
		},
		config.Dispatch.CronCacheSize,
		options,
	)
	if err != nil {
		return nil, err
	}
	s := &workerRuntime{
		clientState: state,
		config:      config,
		consumerID:  uuid.NewString(),
		handlers:    make(map[handlerKey]handler),
		forceStop:   make(chan struct{}),
	}

	s.queues = make(map[string]*queueRuntime, len(config.Queues))
	for _, settings := range config.Queues {
		q := &queueRuntime{
			scheduler: s,
			settings:  settings,
			store:     state.queueStores[settings.Name].store,
		}
		s.queues[settings.Name] = q
		if config.Role&RoleWorker != 0 && subscribesToQueue(settings.Name, config.Execution.Queues) {
			s.workers = append(s.workers, q)
		}
		if config.Role&RoleDispatcher != 0 && subscribesToQueue(settings.Name, config.Dispatch.Queues) {
			s.dispatchers = append(s.dispatchers, q)
		}
	}

	return s, nil
}
