package config

import (
	"sync"

	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/validator"
	"github.com/kochabx/kit/log"
)

// Config manages application configuration
type Config struct {
	mu       sync.RWMutex        // protects concurrent access to target
	viper    *viper.Viper        // viper instance for configuration management
	validate validator.Validator // validator for configuration validation
	target   any                 // target is the destination where the configuration will be unmarshalled
	loader   Loader              // loader is responsible for loading configuration
	watch    bool                // whether to automatically watch for configuration changes
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
		watch:    true,
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	// Create default FileLoader if no loader is provided
	if c.loader == nil {
		c.loader = NewFileLoader("config.yaml", []string{"."}, c.viper, c.validate)
	}

	return c
}

// Load reads the configuration using the configured loader
func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.loader.Load(c.target); err != nil {
		return err
	}

	return nil
}

// Reload reloads the configuration from the loader
func (c *Config) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.loader.Load(c.target); err != nil {
		return err
	}

	return nil
}

// Watch sets up automatic configuration watching if enabled
func (c *Config) Watch() error {
	return c.loader.Watch(func() {
		log.Info().Msg("config change detected")

		// Attempt to reload configuration
		if err := c.Reload(); err != nil {
			log.Error().Err(err).Msg("failed to reload config after change")
			return
		}

		log.Info().Msg("config reloaded successfully")
	})
}

// GetViper returns the underlying viper instance if the loader is a FileLoader
// This is provided for backward compatibility
func (c *Config) GetViper() *viper.Viper {
	return c.viper
}
