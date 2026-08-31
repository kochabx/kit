// Package config loads validated, immutable configuration snapshots.
package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/viper"

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
	v       *viper.Viper
	options options

	mu      sync.Mutex
	current atomic.Pointer[T]

	watching atomic.Bool
}

// New creates a typed configuration. By default it reads config.yaml from the
// working directory and enables environment variable overrides.
func New[T any](options ...Option) (*Config[T], error) {
	if reflect.TypeFor[T]().Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: target type must be a struct", ErrInvalidOptions)
	}
	settings := defaultSettings()
	for _, modify := range options {
		if modify == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidOptions)
		}
		if err := modify(&settings); err != nil {
			return nil, err
		}
	}
	if settings.hasRemote {
		var err error
		settings.viper, err = remoteViper(settings.viper, settings.remote)
		if err != nil {
			return nil, err
		}
	} else if settings.viper == nil {
		settings.viper = defaultViper()
	}
	return &Config[T]{
		v:       settings.viper,
		options: settings.options,
	}, nil
}

// Load reads, validates, and atomically publishes a configuration snapshot.
func (c *Config[T]) Load(ctx context.Context) (*T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.read(); err != nil {
		return nil, err
	}
	return c.publish(ctx)
}

func defaultViper() *viper.Viper {
	v := viper.New()
	v.SetConfigFile("config.yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v
}

func remoteViper(v *viper.Viper, remote remoteOptions) (*viper.Viper, error) {
	if v == nil {
		v = defaultViper()
	}
	v.SetConfigType(remote.configType)
	if err := v.AddRemoteProvider(remote.provider, remote.endpoint, remote.path); err != nil {
		return nil, fmt.Errorf("%w: add remote provider: %w", ErrInvalidOptions, err)
	}
	return v, nil
}

func (c *Config[T]) read() error {
	var err error
	switch c.options.source {
	case SourceValues:
		return nil
	case SourceFile:
		err = c.v.ReadInConfig()
	case SourceRemote:
		err = c.v.ReadRemoteConfig()
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
	filename := c.v.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("%w: no configuration file was used", ErrRead)
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("%w: read %s: %w", ErrRead, filename, err)
	}
	if err := c.v.ReadConfig(bytes.NewBufferString(os.ExpandEnv(string(content)))); err != nil {
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
	if err := c.v.Unmarshal(candidate); err != nil {
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
