package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"time"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type Codec[T any] interface {
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Marshal(v T) ([]byte, error) { return json.Marshal(v) }
func (jsonCodec[T]) Unmarshal(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

type Definition[T any] struct {
	name    string
	codec   Codec[T]
	timeout time.Duration
	retry   RetryPolicy
}

type definitionConfig struct {
	timeout time.Duration
	retry   RetryPolicy
}
type DefinitionOption func(*definitionConfig)

func Define[T any](name string, opts ...DefinitionOption) Definition[T] {
	return DefineWithCodec(name, jsonCodec[T]{}, opts...)
}

func DefineWithCodec[T any](name string, codec Codec[T], opts ...DefinitionOption) Definition[T] {
	c := definitionConfig{timeout: 5 * time.Minute, retry: Exponential{MaxAttempts: 4, Initial: time.Second, Max: time.Hour, Multiplier: 2, Jitter: true}}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return Definition[T]{name: name, codec: codec, timeout: c.timeout, retry: c.retry}
}

func WithTimeout(v time.Duration) DefinitionOption {
	return func(c *definitionConfig) { c.timeout = v }
}
func WithRetry(v RetryPolicy) DefinitionOption { return func(c *definitionConfig) { c.retry = v } }

type HandlerFunc[T any] func(context.Context, T) error

func Handle[T any](s *Scheduler, d Definition[T], fn HandlerFunc[T]) error {
	if !validName.MatchString(d.name) {
		return fmt.Errorf("invalid task name %q", d.name)
	}
	if d.codec == nil || fn == nil || d.timeout <= 0 || d.retry == nil {
		return fmt.Errorf("invalid definition %q", d.name)
	}
	fingerprint, err := definitionFingerprint(d)
	if err != nil {
		return fmt.Errorf("fingerprint definition %q: %w", d.name, err)
	}
	return s.register(d.name, handler{
		timeout:     d.timeout,
		retry:       d.retry,
		fingerprint: fingerprint,
		run: func(ctx context.Context, payload []byte) error {
			v, err := d.codec.Unmarshal(payload)
			if err != nil {
				return Permanent(fmt.Errorf("decode payload: %w", err))
			}
			return fn(ctx, v)
		},
	})
}

func definitionFingerprint[T any](d Definition[T]) (string, error) {
	retry, err := json.Marshal(d.retry)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Name      string
		Timeout   int64
		CodecType string
		RetryType string
		Retry     json.RawMessage
	}{d.name, d.timeout.Nanoseconds(), reflect.TypeOf(d.codec).String(), reflect.TypeOf(d.retry).String(), retry})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical)), nil
}
