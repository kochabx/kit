package config

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch starts a cancellable, debounced fsnotify watcher and delivers validated
// configuration changes to handler until ctx is canceled or Close is called.
func (c *Config[T]) Watch(ctx context.Context, handler func(Event[T])) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidOptions)
	}
	if handler == nil {
		return fmt.Errorf("%w: watch handler is required", ErrInvalidOptions)
	}
	filename, err := c.acquireWatch()
	if err != nil {
		return err
	}
	fw, err := c.openFileWatcher(filename)
	if err != nil {
		c.watching.Store(false)
		return err
	}

	eventsCh := make(chan Event[T])
	watchCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		cancel()
		_ = fw.close()
		c.watching.Store(false)
		return ErrClosed
	}
	c.watchCancel = cancel
	c.watchWG.Go(func() {
		defer close(eventsCh)
		defer c.finishWatch(cancel)
		defer func() { _ = fw.close() }()
		c.runFileWatchLoop(watchCtx, fw, func(ev Event[T]) bool {
			select {
			case eventsCh <- ev:
				return true
			case <-watchCtx.Done():
				return false
			}
		})
	})
	c.mu.Unlock()

	go func() {
		for ev := range eventsCh {
			handler(ev)
		}
	}()
	return nil
}

func (c *Config[T]) acquireWatch() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return "", ErrClosed
	}
	if c.current.Load() == nil {
		return "", ErrNotLoaded
	}
	if c.watching.Load() {
		return "", ErrAlreadyWatching
	}
	filename := c.options.viper.ConfigFileUsed()
	if filename == "" {
		return "", fmt.Errorf("%w: no configuration file was used", ErrRead)
	}
	if !c.watching.CompareAndSwap(false, true) {
		return "", ErrAlreadyWatching
	}
	return filename, nil
}

func (c *Config[T]) finishWatch(cancel context.CancelFunc) {
	c.mu.Lock()
	c.watchCancel = nil
	c.mu.Unlock()
	c.watching.Store(false)
	cancel()
}

type fileWatcher struct {
	watcher        *fsnotify.Watcher
	configFile     string
	realConfigFile string
}

func (c *Config[T]) openFileWatcher(filename string) (*fileWatcher, error) {
	absFile, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve config path %s: %w", ErrRead, filename, err)
	}
	configFile := filepath.Clean(absFile)
	configDir := filepath.Dir(configFile)
	realConfigFile, _ := filepath.EvalSymlinks(configFile)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("%w: create file watcher: %w", ErrRead, err)
	}
	if err := watcher.Add(configDir); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("%w: watch dir %s: %w", ErrRead, configDir, err)
	}
	return &fileWatcher{
		watcher:        watcher,
		configFile:     configFile,
		realConfigFile: realConfigFile,
	}, nil
}

func (fw *fileWatcher) close() error {
	return fw.watcher.Close()
}

func (fw *fileWatcher) matches(event fsnotify.Event) bool {
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) &&
		!event.Has(fsnotify.Rename) && !event.Has(fsnotify.Remove) {
		return false
	}
	if filepath.Clean(event.Name) == fw.configFile {
		if currentReal, err := filepath.EvalSymlinks(fw.configFile); err == nil && currentReal != "" {
			fw.realConfigFile = currentReal
		} else if event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
			fw.realConfigFile = ""
		}
		return true
	}
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
		currentReal, _ := filepath.EvalSymlinks(fw.configFile)
		if currentReal != "" && currentReal != fw.realConfigFile {
			fw.realConfigFile = currentReal
			return true
		}
	}
	return false
}

func (c *Config[T]) runFileWatchLoop(ctx context.Context, fw *fileWatcher, emit func(Event[T]) bool) {
	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			if !fw.matches(event) {
				continue
			}
			if c.options.watchDelay <= 0 {
				if ev, changed := c.reload(ctx); changed {
					if !emit(ev) {
						return
					}
				}
				continue
			}
			if timer == nil {
				timer = time.NewTimer(c.options.watchDelay)
			} else {
				timer.Reset(c.options.watchDelay)
			}
			timerC = timer.C
		case <-timerC:
			timerC = nil
			if ev, changed := c.reload(ctx); changed {
				if !emit(ev) {
					return
				}
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			if !emit(Event[T]{Previous: c.Current(), Err: fmt.Errorf("%w: watch: %w", ErrRead, err)}) {
				return
			}
		}
	}
}

func (c *Config[T]) reload(ctx context.Context) (Event[T], bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return Event[T]{}, false
	}
	previous := c.current.Load()
	if err := c.read(); err != nil {
		return Event[T]{Previous: previous, Err: err}, true
	}
	next, err := c.decode(ctx)
	if err != nil {
		return Event[T]{Previous: previous, Err: err}, true
	}
	if reflect.DeepEqual(previous, next) {
		return Event[T]{}, false
	}
	c.current.Store(next)
	return Event[T]{Previous: previous, Current: next}, true
}
