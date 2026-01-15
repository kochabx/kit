package config

import (
	"path"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/tag"
	"github.com/kochabx/kit/core/validator"
	"github.com/kochabx/kit/errors"
)

// FileLoader loads configuration from file
type FileLoader struct {
	viper    *viper.Viper
	validate validator.Validator
	name     string
	paths    []string
}

// NewFileLoader creates a new file loader
func NewFileLoader(name string, paths []string, v *viper.Viper, validate validator.Validator) *FileLoader {
	// Determine config type from file extension
	extension := path.Ext(name)
	configType := strings.TrimPrefix(extension, ".")

	// Add configuration paths to viper
	for _, configPath := range paths {
		v.AddConfigPath(configPath)
	}

	v.SetConfigName(name)
	v.SetConfigType(configType)

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	return &FileLoader{
		viper:    v,
		paths:    paths,
		name:     name,
		validate: validate,
	}
}

// Load implements Loader interface
func (l *FileLoader) Load(target any) error {
	// Apply default values from struct tags BEFORE unmarshalling
	// This ensures that fields not present in config file get their defaults
	if err := tag.ApplyDefaults(target); err != nil {
		return errors.New(500, "failed to apply defaults: %v", err)
	}

	if err := l.viper.ReadInConfig(); err != nil {
		return errors.New(404, "config file not found: %v", err)
	}

	if err := l.viper.Unmarshal(target); err != nil {
		return errors.New(500, "config parse error: %v", err)
	}

	// Validate configuration
	if l.validate != nil {
		if err := l.validate.Struct(target); err != nil {
			return errors.New(400, "config validation failed: %v", err)
		}
	}

	return nil
}

// Watch implements Loader interface
func (l *FileLoader) Watch(callback func()) error {
	l.viper.OnConfigChange(func(e fsnotify.Event) {
		if callback != nil {
			callback()
		}
	})

	l.viper.WatchConfig()
	return nil
}
