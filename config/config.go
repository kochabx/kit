// Package config loads and hot-reloads validated, immutable configuration snapshots backed by Viper.
package config

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/kochabx/kit/x/defaults"
)

var envPlaceholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Event reports one watched reload attempt. Current is non-nil only when a new valid snapshot was published.
type Event[T any] struct {
	Previous *T
	Current  *T
	Err      error
}

// Config loads typed file configuration via Viper and atomically publishes read-only *T snapshots.
type Config[T any] struct {
	options options

	mu          sync.Mutex
	current     atomic.Pointer[T]
	watching    atomic.Bool
	closed      atomic.Bool
	watchCancel context.CancelFunc
	watchWG     sync.WaitGroup
}

// New creates a typed Config loader targeting struct type T.
// By default it reads config.yaml from the working directory and enables environment variable overrides.
func New[T any](opts ...Option) (*Config[T], error) {
	if reflect.TypeFor[T]().Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: target type must be a struct", ErrInvalidOptions)
	}
	configured, err := newOptions(opts...)
	if err != nil {
		return nil, err
	}
	return &Config[T]{
		options: configured,
	}, nil
}

// Load reads, decodes, validates, and atomically publishes a configuration snapshot.
func (c *Config[T]) Load(ctx context.Context) (*T, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrInvalidOptions)
	}
	if c.closed.Load() {
		return nil, ErrClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return nil, ErrClosed
	}
	if c.watching.Load() {
		return nil, ErrAlreadyWatching
	}
	if err := c.read(); err != nil {
		return nil, err
	}
	return c.publish(ctx)
}

// Current returns the latest validated snapshot, or nil before the first Load.
func (c *Config[T]) Current() *T {
	return c.current.Load()
}

// Viper exposes the underlying *viper.Viper instance. Do not mutate it concurrently during Load or Watch.
func (c *Config[T]) Viper() *viper.Viper {
	return c.options.viper
}

// Close cancels active watchers, closes file handles, and waits for watcher goroutines to exit.
func (c *Config[T]) Close() error {
	c.mu.Lock()
	c.closed.Store(true)
	cancel := c.watchCancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.watchWG.Wait()
	return nil
}

func (c *Config[T]) read() error {
	if err := c.options.viper.ReadInConfig(); err != nil {
		return fmt.Errorf("%w: %w", ErrRead, err)
	}
	return nil
}

func tagNameDecodeOption(tag string) viper.DecoderConfigOption {
	return func(dc *mapstructure.DecoderConfig) {
		dc.TagName = tag
	}
}

func envExpandDecodeHook() viper.DecoderConfigOption {
	return func(dc *mapstructure.DecoderConfig) {
		hook := func(from, _ reflect.Type, data any) (any, error) {
			if from.Kind() != reflect.String {
				return data, nil
			}
			s, ok := data.(string)
			if !ok || !strings.Contains(s, "${") {
				return data, nil
			}
			return envPlaceholderPattern.ReplaceAllStringFunc(s, func(match string) string {
				return os.Getenv(match[2 : len(match)-1])
			}), nil
		}
		if dc.DecodeHook != nil {
			dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(hook, dc.DecodeHook)
		} else {
			dc.DecodeHook = hook
		}
	}
}

func (c *Config[T]) publish(ctx context.Context) (*T, error) {
	candidate, err := c.decode(ctx)
	if err != nil {
		return nil, err
	}
	c.current.Store(candidate)
	return candidate, nil
}

func (c *Config[T]) decode(ctx context.Context) (*T, error) {
	candidate := new(T)
	if err := defaults.Apply(candidate); err != nil {
		return nil, fmt.Errorf("%w: apply defaults: %w", ErrDecode, err)
	}
	if err := c.options.viper.Unmarshal(candidate, c.options.decodeOpts...); err != nil {
		return nil, fmt.Errorf("%w: unmarshal: %w", ErrDecode, err)
	}
	if c.options.validator != nil {
		if err := c.options.validator.Struct(ctx, candidate); err != nil {
			return nil, fmt.Errorf("%w: tags: %w", ErrValidation, err)
		}
	}
	if custom, ok := any(candidate).(Validatable); ok {
		if err := custom.Validate(ctx); err != nil {
			return nil, fmt.Errorf("%w: custom: %w", ErrValidation, err)
		}
	}
	return candidate, nil
}
