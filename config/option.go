package config

import (
	"github.com/kochabx/kit/core/validator"
	"github.com/spf13/viper"
)

// Option customizes a Config.
type Option func(*Config)

// WithViper uses v for the default file loader.
func WithViper(v *viper.Viper) Option {
	return func(c *Config) {
		c.viper = v
	}
}

// WithValidator uses v to validate loaded configuration.
func WithValidator(v validator.Validator) Option {
	return func(c *Config) {
		c.validate = v
	}
}

// WithLoader replaces the default file loader.
func WithLoader(loader Loader) Option {
	return func(c *Config) {
		c.loader = loader
	}
}

// WithOnChange registers fn to run after a successful watched reload.
func WithOnChange(fn func()) Option {
	return func(c *Config) {
		c.onChange = fn
	}
}
