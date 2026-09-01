// Package config loads validated, immutable configuration snapshots.
package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/kochabx/kit/core/defaults"
)

type validatable interface {
	Validate(context.Context) error
}

// Event reports one watched reload attempt. Current is non-nil only when a
// new valid snapshot was published.
type Event[T any] struct {
	Previous *T
	Current  *T
	Err      error
}

// Config loads typed configuration and publishes its latest valid snapshot.
// Published values must be treated as read-only.
type Config[T any] struct {
	options options

	mu      sync.Mutex
	current atomic.Pointer[T]

	watching atomic.Bool
}

// New creates a typed configuration. By default it reads config.yaml from the
// working directory and enables environment variable overrides.
func New[T any](opts ...Option) (*Config[T], error) {
	if reflect.TypeFor[T]().Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: target type must be a struct", ErrInvalidOptions)
	}
	options, err := newOptions(opts...)
	if err != nil {
		return nil, err
	}
	return &Config[T]{
		options: options,
	}, nil
}

// Load reads, validates, and atomically publishes a configuration snapshot.
// The context must not be nil.
func (c *Config[T]) Load(ctx context.Context) (*T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watching.Load() {
		return nil, ErrAlreadyWatching
	}
	if err := c.read(); err != nil {
		return nil, err
	}
	return c.publish(ctx)
}

func (c *Config[T]) read() error {
	var err error
	switch c.options.source {
	case SourceValues:
		return nil
	case SourceFile:
		err = c.options.viper.ReadInConfig()
	case SourceRemote:
		err = c.options.viper.ReadRemoteConfig()
	default:
		return fmt.Errorf("%w: source %q", ErrUnsupportedSource, c.options.source)
	}
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRead, c.options.source, err)
	}
	if c.options.source == SourceFile {
		return c.expandEnvironment()
	}
	return nil
}

func (c *Config[T]) expandEnvironment() error {
	filename := c.options.viper.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("%w: no configuration file was used", ErrRead)
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("%w: read %s: %w", ErrRead, filename, err)
	}
	if err := c.options.viper.ReadConfig(bytes.NewBufferString(os.ExpandEnv(string(content)))); err != nil {
		return fmt.Errorf("%w: expand environment variables: %w", ErrRead, err)
	}
	return nil
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
	if err := c.options.viper.Unmarshal(candidate); err != nil {
		return nil, fmt.Errorf("%w: unmarshal: %w", ErrDecode, err)
	}
	if err := c.options.validator.Struct(ctx, candidate); err != nil {
		return nil, fmt.Errorf("%w: tags: %w", ErrValidation, err)
	}
	if custom, ok := any(candidate).(validatable); ok {
		if err := custom.Validate(ctx); err != nil {
			return nil, fmt.Errorf("%w: custom: %w", ErrValidation, err)
		}
	}
	return candidate, nil
}
