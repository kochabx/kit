package config

import (
	"crypto/sha256"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/kochabx/kit/core/defaults"
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
	configName := strings.TrimSuffix(name, extension)
	configType := strings.TrimPrefix(extension, ".")

	// Add configuration paths to viper
	for _, configPath := range paths {
		v.AddConfigPath(configPath)
	}

	v.SetConfigName(configName)
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
	if err := l.viper.ReadInConfig(); err != nil {
		return errors.New(404, "config file not found: %v", err)
	}

	if err := l.viper.Unmarshal(target); err != nil {
		return errors.New(500, "config parse error: %v", err)
	}

	// Apply default values from struct tags AFTER unmarshalling
	// This fills in defaults only for zero-value fields not present in config file
	if err := defaults.Apply(target); err != nil {
		return errors.New(500, "failed to apply defaults: %v", err)
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
	notify := make(chan struct{}, 1)

	go func() {
		lastHash := l.fileHash()
		timer := time.NewTimer(0)
		timer.Stop()

		for {
			select {
			case <-notify:
				timer.Reset(100 * time.Millisecond)
			case <-timer.C:
				if h := l.fileHash(); h != [sha256.Size]byte{} && h != lastHash {
					lastHash = h
					callback()
				}
			}
		}
	}()

	l.viper.OnConfigChange(func(e fsnotify.Event) {
		select {
		case notify <- struct{}{}:
		default:
		}
	})

	l.viper.WatchConfig()
	return nil
}

// fileHash computes the SHA-256 checksum of the config file.
func (l *FileLoader) fileHash() [sha256.Size]byte {
	f, err := os.Open(l.viper.ConfigFileUsed())
	if err != nil {
		return [sha256.Size]byte{}
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}
