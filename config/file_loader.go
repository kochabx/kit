package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/defaults"
	"github.com/kochabx/kit/core/validator"
)

// FileLoader loads and watches a file-backed configuration through Viper.
type FileLoader struct {
	viper     *viper.Viper
	validate  validator.Validator
	watchOnce sync.Once
}

// NewFileLoader creates a file loader for name and searches paths in order.
// Environment variables override file values using underscores for nested keys.
func NewFileLoader(name string, paths []string, v *viper.Viper, validate validator.Validator) *FileLoader {
	extension := path.Ext(name)
	configName := strings.TrimSuffix(name, extension)
	configType := strings.TrimPrefix(extension, ".")

	for _, configPath := range paths {
		v.AddConfigPath(configPath)
	}

	v.SetConfigName(configName)
	v.SetConfigType(configType)

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	return &FileLoader{
		viper:    v,
		validate: validate,
	}
}

// Load reads, expands, decodes, defaults, and validates the configuration.
func (l *FileLoader) Load(target any) error {
	if err := l.viper.ReadInConfig(); err != nil {
		return err
	}

	if err := l.readExpandedConfigFile(); err != nil {
		return err
	}

	if err := l.viper.Unmarshal(target); err != nil {
		return err
	}

	if err := defaults.Apply(target); err != nil {
		return err
	}

	if l.validate != nil {
		if err := l.validate.Struct(context.Background(), target); err != nil {
			return err
		}
	}

	return nil
}

func (l *FileLoader) readExpandedConfigFile() error {
	configFile := l.viper.ConfigFileUsed()
	if configFile == "" {
		return nil
	}

	content, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	expanded := os.ExpandEnv(string(content))
	if err := l.viper.ReadConfig(bytes.NewBufferString(expanded)); err != nil {
		return err
	}

	return nil
}

// Watch starts a debounced file watcher. Repeated calls are ignored.
func (l *FileLoader) Watch(callback func()) error {
	if callback == nil {
		return errors.New("config watch callback must not be nil")
	}

	l.watchOnce.Do(func() {
		var mu sync.Mutex
		var timer *time.Timer
		var generation uint64

		l.viper.OnConfigChange(func(fsnotify.Event) {
			mu.Lock()
			defer mu.Unlock()
			generation++
			current := generation
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(100*time.Millisecond, func() {
				mu.Lock()
				defer mu.Unlock()
				if current == generation {
					callback()
				}
			})
		})
		l.viper.WatchConfig()
	})
	return nil
}
