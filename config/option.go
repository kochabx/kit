package config

import (
	"github.com/kochabx/kit/core/validator"
	"github.com/spf13/viper"
)

// Option is a function that configures a Config
type Option func(*Config)

// WithViper sets a custom viper instance
func WithViper(v *viper.Viper) Option {
	return func(c *Config) {
		c.viper = v
	}
}

// WithValidator sets a custom validator
func WithValidator(v validator.Validator) Option {
	return func(c *Config) {
		c.validate = v
	}
}

// WithLoader sets the configuration loader
func WithLoader(loader Loader) Option {
	return func(c *Config) {
		c.loader = loader
	}
}

// WithOnChange sets a callback that is invoked after configuration is successfully reloaded
func WithOnChange(fn func()) Option {
	return func(c *Config) {
		c.onChange = fn
	}
}
