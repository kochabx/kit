package config

import (
	"context"
	"fmt"
	"strings"
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
	source    Source
	viper     *viper.Viper
	validator validator.Validator
	remote    *remoteOptions
}

type remoteOptions struct {
	Provider   string        `validate:"required"`
	Endpoint   string        `validate:"required"`
	Path       string        `validate:"required"`
	ConfigType string        `validate:"required"`
	Interval   time.Duration `validate:"gt=0"`
}

func defaultOptions() options {
	return options{
		source:    SourceFile,
		validator: validator.Validate,
	}
}

func newOptions(opts ...Option) (options, error) {
	configured := defaultOptions()
	for _, opt := range opts {
		if opt == nil {
			return configured, fmt.Errorf("%w: nil option", ErrInvalidOptions)
		}
		if err := opt(&configured); err != nil {
			return configured, err
		}
	}
	if err := configured.validate(); err != nil {
		return configured, err
	}
	if err := configured.initialize(); err != nil {
		return configured, err
	}
	return configured, nil
}

func (o *options) validate() error {
	if o.remote != nil {
		if err := validator.Validate.Struct(context.Background(), o.remote); err != nil {
			return err
		}
	}
	if o.remote != nil && o.source != SourceRemote {
		return fmt.Errorf("%w: remote provider requires remote source", ErrInvalidOptions)
	}
	if o.source == SourceRemote && o.remote == nil {
		return fmt.Errorf("%w: remote source requires WithRemote", ErrInvalidOptions)
	}
	return nil
}

func (o *options) initialize() error {
	if o.remote != nil {
		var err error
		o.viper, err = remoteViper(o.viper, *o.remote)
		return err
	}
	if o.viper == nil {
		o.viper = defaultViper()
	}
	return nil
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
	v.SetConfigType(remote.ConfigType)
	if err := v.AddRemoteProvider(remote.Provider, remote.Endpoint, remote.Path); err != nil {
		return nil, fmt.Errorf("%w: add remote provider: %w", ErrInvalidOptions, err)
	}
	return v, nil
}

// Option customizes Config construction.
type Option func(*options) error

// WithSource selects the configuration source. The default is SourceFile.
func WithSource(source Source) Option {
	return func(options *options) error {
		switch source {
		case SourceFile, SourceRemote, SourceValues:
			options.source = source
			return nil
		default:
			return fmt.Errorf("%w: source %q", ErrUnsupportedSource, source)
		}
	}
}

// WithViper uses a caller-configured Viper instance. This is intended for
// flags, aliases, Set, custom file locations, and remote providers.
func WithViper(value *viper.Viper) Option {
	return func(options *options) error {
		if value == nil {
			return fmt.Errorf("%w: viper is nil", ErrInvalidOptions)
		}
		options.viper = value
		return nil
	}
}

// WithValidator replaces the default project validator.
func WithValidator(value validator.Validator) Option {
	return func(options *options) error {
		if value == nil {
			return fmt.Errorf("%w: validator is nil", ErrInvalidOptions)
		}
		options.validator = value
		return nil
	}
}

// WithRemote configures a remote provider and how often WatchRemote refreshes
// its configuration.
func WithRemote(provider, endpoint, path, configType string, interval time.Duration) Option {
	return func(options *options) error {
		options.source = SourceRemote
		options.remote = &remoteOptions{
			Provider:   provider,
			Endpoint:   endpoint,
			Path:       path,
			ConfigType: configType,
			Interval:   interval,
		}
		return nil
	}
}
