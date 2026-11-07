package scheduler

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// ClientConfig contains only producer and management settings.
type ClientConfig struct {
	Namespace        string              `json:"namespace" default:"scheduler" validate:"required,max=128,ascii"`
	Queues           []string            `json:"queues"`
	MaxPayloadBytes  int                 `json:"max_payload_bytes" default:"1048576" validate:"gt=0"`
	OperationTimeout time.Duration       `json:"operation_timeout" default:"5s" validate:"gt=0"`
	ReplayInterval   time.Duration       `json:"replay_interval" validate:"gte=0"`
	Retention        RetentionConfig     `json:"retention"`
	Observability    ObservabilityConfig `json:"observability"`
}

// Client submits and manages jobs. It never starts workers or background schedulers.
// The Redis client is borrowed and remains open after Close.
type Client struct{ state *clientState }

func NewClient(redisClient redis.UniversalClient, config ClientConfig) (*Client, error) {
	state, err := newClientState(redisClient, config, 1024, nil)
	if err != nil {
		return nil, err
	}
	return &Client{state: state}, nil
}

func (c *Client) Close() error {
	if c == nil || c.state == nil {
		return ErrInvalidArgument
	}
	return c.state.Close()
}
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.state == nil {
		return ErrInvalidArgument
	}
	return c.state.Ping(ctx)
}
func (c *Client) Jobs() Jobs {
	if c == nil || c.state == nil {
		return Jobs{}
	}
	return c.state.Jobs()
}
func (c *Client) Schedules() Schedules {
	if c == nil || c.state == nil {
		return Schedules{}
	}
	return c.state.Schedules()
}
func (c *Client) Queue(name string) (*Queue, error) {
	if c == nil || c.state == nil {
		return nil, ErrInvalidArgument
	}
	return c.state.Queue(name)
}
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	if c == nil || c.state == nil {
		return Stats{}, ErrInvalidArgument
	}
	return c.state.Stats(ctx)
}

func (c *Client) Enqueue[T any](ctx context.Context, task Task[T], payload T, opts ...EnqueueOption) (*Job, error) {
	if c == nil {
		return nil, ErrInvalidArgument
	}
	producer, err := task.bind(c.state)
	if err != nil {
		return nil, err
	}
	return producer.Enqueue(ctx, payload, opts...)
}
func (c *Client) EnqueueBatch[T any](ctx context.Context, task Task[T], payloads []T, opts ...EnqueueOption) []EnqueueResult {
	if c != nil {
		if producer, err := task.bind(c.state); err == nil {
			return producer.EnqueueBatch(ctx, payloads, opts...)
		} else {
			return batchError(len(payloads), err)
		}
	}
	return batchError(len(payloads), ErrInvalidArgument)
}
func batchError(n int, err error) []EnqueueResult {
	out := make([]EnqueueResult, n)
	for i := range out {
		out[i].Err = err
	}
	return out
}

// Create creates a typed recurrence through the Client's Schedules scope.
func (schedules Schedules) Create[T any](ctx context.Context, task Task[T], payload T, spec CronSpec) (*Schedule, error) {
	producer, err := task.bind(schedules.scheduler)
	if err != nil {
		return nil, err
	}
	return producer.Schedule(ctx, payload, spec)
}
