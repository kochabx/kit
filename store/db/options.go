package db

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/kochabx/kit/log"
)

type openOptions struct {
	plugins []gorm.Plugin
	logger  *log.Logger
}

// Option supplies a runtime dependency or advanced GORM extension.
type Option func(*openOptions) error

// WithLogger enables GORM logging and sends its output to logger.
func WithLogger(logger *log.Logger) Option {
	return func(opts *openOptions) error {
		if logger == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidOption)
		}
		opts.logger = logger
		return nil
	}
}

// WithPlugins installs GORM plugins before the initial connection check.
func WithPlugins(plugins ...gorm.Plugin) Option {
	return func(opts *openOptions) error {
		for index, plugin := range plugins {
			if plugin == nil {
				return fmt.Errorf("%w: nil plugin at index %d", ErrInvalidOption, index)
			}
		}
		opts.plugins = append(opts.plugins, plugins...)
		return nil
	}
}
