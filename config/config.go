package config

import (
	"errors"
	"reflect"
	"sync"

	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/validator"
	"github.com/kochabx/kit/log"
)

const (
	defaultFileName   = "config.yaml"
	defaultSearchPath = "."
)

// Config loads configuration into a target and keeps it synchronized with its source.
type Config struct {
	mu       sync.Mutex
	viper    *viper.Viper
	validate validator.Validator
	target   any
	loader   Loader
	onChange func()
}

// New creates a Config for target.
// Without a custom loader, it reads the following default file:
//   - filename: "config.yaml"
//   - paths: ["."]
func New(target any, opts ...Option) *Config {
	c := &Config{
		viper:    viper.New(),
		validate: validator.Validate,
		target:   target,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.loader == nil {
		c.loader = NewFileLoader(defaultFileName, []string{defaultSearchPath}, c.viper, c.validate)
	}

	return c
}

// Load replaces target only after the new configuration has loaded successfully.
func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	candidate, commit, err := prepareTarget(c.target)
	if err != nil {
		return err
	}
	if err := c.loader.Load(candidate); err != nil {
		return err
	}
	commit()
	return nil
}

// Watch reloads the target whenever the underlying source changes.
func (c *Config) Watch() error {
	return c.loader.Watch(func() {
		if err := c.Load(); err != nil {
			log.Error().Err(err).Msg("failed to reload config after change")
			return
		}

		log.Info().Msg("config reloaded successfully")

		if c.onChange != nil {
			c.onChange()
		}
	})
}

// prepareTarget creates an empty value with the target's concrete type.
// The returned commit function publishes the value after a successful load.
func prepareTarget(target any) (candidate any, commit func(), err error) {
	if target == nil {
		return nil, nil, errors.New("config target must be a non-nil pointer")
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Pointer || targetValue.IsNil() {
		return nil, nil, errors.New("config target must be a non-nil pointer")
	}

	candidateValue := reflect.New(targetValue.Elem().Type())
	return candidateValue.Interface(), func() {
		targetValue.Elem().Set(candidateValue.Elem())
	}, nil
}
