package config

import (
	"sync"

	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/validator"
	"github.com/kochabx/kit/log"
)

const (
	defaultConfigFileName = "config.yaml"
	defaultConfigPaths    = "."
)

// Config manages application configuration
type Config struct {
	mu       sync.RWMutex        // protects concurrent access to target
	viper    *viper.Viper        // viper instance for configuration management
	validate validator.Validator // validator for configuration validation
	target   any                 // target is the destination where the configuration will be unmarshalled
	loader   Loader              // loader is responsible for loading configuration
	onChange func()              // onChange is the user-defined callback invoked after config reload
}

// New creates a new Config instance with the given options
// If no loader is provided, a default FileLoader will be created with:
//   - filename: "config.yaml"
//   - paths: ["."]
func New(target any, opts ...Option) *Config {
	c := &Config{
		viper:    viper.New(),
		validate: validator.Validate,
		target:   target,
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	// Create default FileLoader if no loader is provided
	if c.loader == nil {
		c.loader = NewFileLoader(defaultConfigFileName, []string{defaultConfigPaths}, c.viper, c.validate)
	}

	return c
}

// Load reads the configuration using the configured loader
func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.loader.Load(c.target)
}

// Reload reloads the configuration from the loader
func (c *Config) Reload() error {
	return c.Load()
}

// Watch sets up automatic configuration watching
func (c *Config) Watch() error {
	return c.loader.Watch(func() {
		// Attempt to reload configuration
		if err := c.Reload(); err != nil {
			log.Error().Err(err).Msg("failed to reload config after change")
			return
		}

		log.Info().Msg("config reloaded successfully")

		if c.onChange != nil {
			c.onChange()
		}
	})
}

// GetViper returns the underlying viper instance
func (c *Config) GetViper() *viper.Viper {
	return c.viper
}

// Set dynamically modifies a configuration value by key, re-unmarshals
// the configuration into the target struct to keep it in sync,
// and persists the change to the configuration file.
// The key uses dot notation for nested values, e.g. "server.port".
func (c *Config) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Set(key, value)

	if err := c.viper.Unmarshal(c.target); err != nil {
		return err
	}

	if err := c.viper.WriteConfig(); err != nil {
		return err
	}

	return nil
}
