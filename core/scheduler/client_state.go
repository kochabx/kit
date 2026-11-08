package scheduler

import (
	"context"
	"fmt"
	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/kochabx/kit/log"
	"github.com/redis/go-redis/v9"
	"slices"
	"sync/atomic"
)

// clientState owns only namespace access, producer limits and management views.
// A Worker owns a separate instance and adds its own execution state.
type clientState struct {
	namespace   *store.Namespace
	config      ClientConfig
	queueStores map[string]*queueView
	queueOrder  []*queueView
	closed      atomic.Bool
	cronCache   *cronCache
	events      *eventBus
}
type queueView struct{ store *store.Queue }

func newClientState(client redis.UniversalClient, config ClientConfig, cacheSize int, workerQueues map[string]store.QueueOptions) (*clientState, error) {
	if isNilInterface(client) {
		return nil, fmt.Errorf("redis client is required: %w", ErrInvalidArgument)
	}
	if err := store.ValidateClient(client); err != nil {
		return nil, err
	}
	config.Queues = slices.Clone(config.Queues)
	if len(config.Queues) == 0 {
		config.Queues = []string{DefaultQueue}
	}
	if err := defaults.Apply(&config); err != nil {
		return nil, fmt.Errorf("client defaults: %w: %w", ErrInvalidArgument, err)
	}
	if err := configValidator.Struct(context.Background(), &config); err != nil {
		return nil, fmt.Errorf("client config: %w: %w", ErrInvalidArgument, err)
	}
	if !validNamespace.MatchString(config.Namespace) {
		return nil, ErrInvalidArgument
	}
	seen := make(map[string]bool, len(config.Queues))
	for _, name := range config.Queues {
		if !validName.MatchString(name) || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate queue %q: %w", name, ErrInvalidArgument)
		}
		seen[name] = true
	}
	if config.Observability.Logger == nil {
		config.Observability.Logger = log.Global()
	}
	state := &clientState{
		namespace:   store.New(client, config.Namespace, config.OperationTimeout),
		config:      config,
		queueStores: make(map[string]*queueView),
		cronCache:   newCronCache(cacheSize),
	}
	for _, name := range config.Queues {
		options := store.QueueOptions{
			DeadRetention:       config.Retention.Dead,
			SubmissionRetention: config.Retention.Submission,
			CompletionRetention: config.Retention.Completion,
		}
		if configured, ok := workerQueues[name]; ok {
			options = configured
		}
		view := &queueView{store: state.namespace.Queue(name, options)}
		state.queueStores[name] = view
		state.queueOrder = append(state.queueOrder, view)
	}
	state.events = newEventBus(config.Observability, config.OperationTimeout)
	return state, nil
}

func (s *clientState) Close() error {
	if !s.closed.Swap(true) {
		s.events.close()
	}
	return nil
}

func (s *clientState) Ping(ctx context.Context) error {
	return s.namespace.Ping(ctx)
}

func (s *clientState) lookupQueue(name string) (*queueView, error) {
	q, ok := s.queueStores[name]
	if !ok {
		return nil, fmt.Errorf("queue %q: %w", name, ErrUnknownQueue)
	}
	return q, nil
}

func (s *clientState) lookupJobQueue(ctx context.Context, id string) (*queueView, error) {
	name, err := s.namespace.JobQueue(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.persistedQueue(name)
}

func (s *clientState) lookupScheduleQueue(ctx context.Context, id string) (*queueView, error) {
	name, err := s.namespace.ScheduleQueue(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.persistedQueue(name)
}

func (s *clientState) persistedQueue(name string) (*queueView, error) {
	if !validName.MatchString(name) {
		return nil, ErrInvalidState
	}
	return s.lookupQueue(name)
}
