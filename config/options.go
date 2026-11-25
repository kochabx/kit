package config

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/kochabx/kit/x/validate"
)

var defaultTagValidator = sync.OnceValues(func() (*validate.Validator, error) {
	return validate.New()
})

// Validator checks struct constraints on a decoded configuration snapshot.
type Validator interface {
	Struct(ctx context.Context, v any) error
}

// Validatable can be implemented by configuration structs for custom cross-field validation.
type Validatable interface {
	Validate(context.Context) error
}

type options struct {
	filePath   string
	viper      *viper.Viper
	expandEnv  bool
	tagName    string
	validator  Validator
	decodeOpts []viper.DecoderConfigOption
	watchDelay time.Duration
}

func defaultOptions() options {
	return options{
		expandEnv:  true,
		tagName:    "json",
		watchDelay: 50 * time.Millisecond,
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
	if configured.viper == nil {
		configured.viper = newViper()
	}
	if configured.filePath != "" {
		configured.viper.SetConfigFile(configured.filePath)
	}
	if configured.validator == nil {
		validator, err := defaultTagValidator()
		if err != nil {
			return configured, fmt.Errorf("%w: init default validator: %w", ErrInvalidOptions, err)
		}
		configured.validator = validator
	}
	resolved := make([]viper.DecoderConfigOption, 0, len(configured.decodeOpts)+2)
	resolved = append(resolved, tagNameDecodeOption(configured.tagName))
	resolved = append(resolved, configured.decodeOpts...)
	if configured.expandEnv {
		resolved = append(resolved, envExpandDecodeHook())
	}
	configured.decodeOpts = resolved
	return configured, nil
}

func newViper() *viper.Viper {
	v := viper.New()
	v.SetConfigFile("config.yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v
}

// Option customizes Config construction.
type Option func(*options) error

// WithFile configures the configuration file path on the underlying Viper instance.
func WithFile(path string) Option {
	return func(o *options) error {
		if path == "" {
			return fmt.Errorf("%w: empty file path", ErrInvalidOptions)
		}
		o.filePath = path
		return nil
	}
}

// WithViper uses a caller-configured Viper instance for flags, aliases, Set, and custom paths.
func WithViper(v *viper.Viper) Option {
	return func(o *options) error {
		if v == nil {
			return fmt.Errorf("%w: viper is nil", ErrInvalidOptions)
		}
		o.viper = v
		return nil
	}
}

// WithExpandEnv toggles ${NAME} placeholder expansion on decoded string config values (default: true).
func WithExpandEnv(enabled bool) Option {
	return func(o *options) error {
		o.expandEnv = enabled
		return nil
	}
}

// WithValidator overrides the default x/validate struct tag validator.
func WithValidator(v Validator) Option {
	return func(o *options) error {
		if v == nil {
			return fmt.Errorf("%w: validator is nil", ErrInvalidOptions)
		}
		o.validator = v
		return nil
	}
}

// WithTag overrides the struct tag name used during Unmarshal (default: "json").
func WithTag(tag string) Option {
	return func(o *options) error {
		if tag == "" {
			return fmt.Errorf("%w: empty tag name", ErrInvalidOptions)
		}
		o.tagName = tag
		return nil
	}
}

// WithDecodeOptions appends custom Viper DecoderConfigOption hooks for Unmarshal.
func WithDecodeOptions(opts ...viper.DecoderConfigOption) Option {
	return func(o *options) error {
		for _, opt := range opts {
			if opt == nil {
				return fmt.Errorf("%w: nil decode option", ErrInvalidOptions)
			}
			o.decodeOpts = append(o.decodeOpts, opt)
		}
		return nil
	}
}

// WithWatchDebounce sets the coalesce delay for file watch events (default: 50ms).
func WithWatchDebounce(delay time.Duration) Option {
	return func(o *options) error {
		if delay < 0 {
			return fmt.Errorf("%w: negative watch debounce", ErrInvalidOptions)
		}
		o.watchDelay = delay
		return nil
	}
}
