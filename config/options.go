package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/validator"
)

// Source identifies how Viper obtains its base configuration.
type Source string

const (
	// SourceFile reads the file configured on Viper.
	SourceFile Source = "file"
	// SourceRemote reads a remote provider configured on Viper.
	SourceRemote Source = "remote"
	// SourceValues uses values already registered through Set, flags,
	// environment variables, aliases, and Viper defaults.
	SourceValues Source = "values"
)

type options struct {
	source         Source
	remoteInterval time.Duration
	validator      validator.Validator
}

type remoteOptions struct {
	provider   string
	endpoint   string
	path       string
	configType string
}

type settings struct {
	viper     *viper.Viper
	options   options
	remote    remoteOptions
	hasRemote bool
}

func defaultSettings() settings {
	return settings{
		options: options{
			source:         SourceFile,
			remoteInterval: 5 * time.Second,
			validator:      validator.Validate,
		},
	}
}

// Option customizes Config construction.
type Option func(*settings) error

// WithSource selects the configuration source. The default is SourceFile.
func WithSource(source Source) Option {
	return func(settings *settings) error {
		switch source {
		case SourceFile, SourceRemote, SourceValues:
			settings.options.source = source
			return nil
		default:
			return fmt.Errorf("%w: source %q", ErrUnsupportedSource, source)
		}
	}
}

// WithRemote configures a remote provider and how often WatchRemote refreshes
// its configuration.
func WithRemote(provider, endpoint, path, configType string, interval time.Duration) Option {
	return func(settings *settings) error {
		if provider == "" {
			return fmt.Errorf("%w: remote provider is required", ErrInvalidOptions)
		}
		if endpoint == "" {
			return fmt.Errorf("%w: remote endpoint is required", ErrInvalidOptions)
		}
		if path == "" {
			return fmt.Errorf("%w: remote path is required", ErrInvalidOptions)
		}
		if configType == "" {
			return fmt.Errorf("%w: remote config type is required", ErrInvalidOptions)
		}
		if interval <= 0 {
			return fmt.Errorf("%w: remote interval must be positive", ErrInvalidOptions)
		}
		settings.options.source = SourceRemote
		settings.options.remoteInterval = interval
		settings.remote = remoteOptions{
			provider:   provider,
			endpoint:   endpoint,
			path:       path,
			configType: configType,
		}
		settings.hasRemote = true
		return nil
	}
}

// WithValidator replaces the default project validator.
func WithValidator(value validator.Validator) Option {
	return func(settings *settings) error {
		if value == nil {
			return fmt.Errorf("%w: validator is nil", ErrInvalidOptions)
		}
		settings.options.validator = value
		return nil
	}
}

// WithViper uses a caller-configured Viper instance. This is intended for
// flags, aliases, Set, custom file locations, and remote providers.
func WithViper(value *viper.Viper) Option {
	return func(settings *settings) error {
		if value == nil {
			return fmt.Errorf("%w: viper is nil", ErrInvalidOptions)
		}
		settings.viper = value
		return nil
	}
}
