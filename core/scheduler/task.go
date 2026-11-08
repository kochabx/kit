package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"regexp"
	"time"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// Codec identifies its wire format and configuration across producer and worker nodes.
type Codec[T any] interface {
	Identity() string
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Identity() string {
	return "json:v1"
}

func (jsonCodec[T]) Marshal(v T) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec[T]) Unmarshal(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// Task is immutable after construction. Custom codecs and retry policies
// must keep their configuration fixed and support concurrent calls.
type Task[T any] struct {
	fingerprint string
	version     string
	queue       string
	name        string
	codec       Codec[T]
	retry       RetryPolicy
	policy      ExecutionPolicy
}

type taskConfig struct {
	version       string
	queue         string
	timeout       time.Duration
	retry         RetryPolicy
	maxRecoveries int
	maxExecutions int
}

type TaskOption func(*taskConfig)

// NewTask validates and freezes task metadata before it can be bound or submitted.
func NewTask[T any](name string, opts ...TaskOption) (Task[T], error) {
	return NewTaskWithCodec(name, jsonCodec[T]{}, opts...)
}

func NewTaskWithCodec[T any](name string, codec Codec[T], opts ...TaskOption) (Task[T], error) {
	c := taskConfig{
		queue:         DefaultQueue,
		version:       "1",
		maxRecoveries: 3, maxExecutions: 100,
		timeout: 5 * time.Minute,
		retry:   Exponential{MaxAttempts: 4, Initial: time.Second, Max: time.Hour, Multiplier: 2, Jitter: true},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if !validName.MatchString(name) ||
		!validName.MatchString(c.queue) ||
		isNilInterface(codec) ||
		isNilInterface(c.retry) ||
		c.maxRecoveries < 0 ||
		c.maxExecutions < 1 ||
		c.timeout <= 0 ||
		!validName.MatchString(c.version) {
		return Task[T]{}, fmt.Errorf("invalid task %q: %w", name, ErrInvalidArgument)
	}
	policy, err := snapshotPolicy(c.timeout, c.retry, c.maxRecoveries, c.maxExecutions)
	if err != nil {
		return Task[T]{}, err
	}
	d := Task[T]{policy: policy, version: c.version, queue: c.queue, name: name, codec: codec, retry: c.retry}
	fingerprint, err := computeFingerprint(d)
	if err != nil {
		return Task[T]{}, fmt.Errorf("fingerprint task %q: %w: %w", name, ErrInvalidArgument, err)
	}
	d.fingerprint = fingerprint
	return d, nil
}

// WithVersion identifies the payload contract; change it when its schema changes.
func WithVersion(version string) TaskOption {
	return func(c *taskConfig) { c.version = version }
}

func (t Task[T]) Version() string {
	return t.version
}

// WithQueue permanently binds a task definition to its execution queue.
func WithQueue(name string) TaskOption {
	return func(c *taskConfig) { c.queue = name }
}

func WithTimeout(v time.Duration) TaskOption {
	return func(c *taskConfig) { c.timeout = v }
}

func WithRetry(v RetryPolicy) TaskOption {
	return func(c *taskConfig) { c.retry = v }
}

type Handler[T any] func(context.Context, T) error

func (s *workerRuntime) registerTaskHandler[T any](d Task[T], fn Handler[T]) error {
	if fn == nil {
		return fmt.Errorf("handler is required: %w", ErrInvalidArgument)
	}
	fingerprint := d.fingerprint
	return s.registerHandler(d.queue, d.name, handler{
		version:       d.version,
		queue:         d.queue,
		retry:         d.retry,
		retryIdentity: d.policy.Retry.Identity,
		fingerprint:   fingerprint,
		run: func(ctx context.Context, payload []byte) error {
			v, err := d.codec.Unmarshal(payload)
			if err != nil {
				return Permanent(fmt.Errorf("decode payload: %w", err))
			}
			return fn(ctx, v)
		},
	})
}

func computeFingerprint[T any](d Task[T]) (string, error) {
	if !validName.MatchString(d.queue) {
		return "", fmt.Errorf("invalid queue %q: %w", d.queue, ErrInvalidArgument)
	}
	if d.codec == nil || d.retry == nil {
		return "", fmt.Errorf("codec and retry policy are required: %w", ErrInvalidArgument)
	}

	identity := d.codec.Identity()
	if identity == "" {
		return "", fmt.Errorf("empty codec identity: %w", ErrInvalidArgument)
	}
	canonical, err := json.Marshal(struct{ Name, Version, CodecIdentity string }{d.name, d.version, identity}, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical)), nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// WithRecoveryLimit limits lease-expiration recoveries independently of business retries.
func WithRecoveryLimit(n int) TaskOption {
	return func(c *taskConfig) { c.maxRecoveries = n }
}

// WithExecutionLimit is the absolute number of executions in one job generation.
func WithExecutionLimit(n int) TaskOption {
	return func(c *taskConfig) { c.maxExecutions = n }
}
