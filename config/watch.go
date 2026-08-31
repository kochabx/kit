package config

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/fsnotify/fsnotify"
)

const fileReloadDelay = 50 * time.Millisecond

// Watch starts Viper's native file watcher. Load must succeed first. Viper
// does not provide a way to stop the watcher, so it lives for the process
// lifetime.
func (c *Config[T]) Watch(handler func(Event[T])) error {
	if err := c.startWatch(SourceFile, handler); err != nil {
		return err
	}

	c.v.OnConfigChange(func(fsnotify.Event) {
		// A truncate-and-write operation can emit an event before the new file
		// content is complete. Keep the reload in Viper's callback goroutine so
		// Viper cannot start processing the next event concurrently.
		time.Sleep(fileReloadDelay)
		c.reload(context.Background(), c.expandEnvironment, handler)
	})
	c.v.WatchConfig()
	return nil
}

// WatchRemote polls a remote source until ctx is canceled. Load must succeed
// first.
func (c *Config[T]) WatchRemote(ctx context.Context, handler func(Event[T])) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidOptions)
	}
	if err := c.startWatch(SourceRemote, handler); err != nil {
		return err
	}

	go func() {
		defer c.stopWatching()
		ticker := time.NewTicker(c.options.remoteInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.reload(ctx, c.read, handler)
			}
		}
	}()
	return nil
}

func (c *Config[T]) startWatch(source Source, handler func(Event[T])) error {
	if handler == nil {
		return fmt.Errorf("%w: watch handler is required", ErrInvalidOptions)
	}
	if c.current.Load() == nil {
		return ErrNotLoaded
	}
	if c.options.source != source {
		return fmt.Errorf("%w: source %q", ErrUnsupportedSource, c.options.source)
	}
	if !c.startWatching() {
		return ErrAlreadyWatching
	}
	return nil
}

func (c *Config[T]) startWatching() bool {
	return c.watching.CompareAndSwap(false, true)
}

func (c *Config[T]) stopWatching() {
	c.watching.Store(false)
}

func (c *Config[T]) reload(
	ctx context.Context,
	refresh func() error,
	handler func(Event[T]),
) {
	previous := c.current.Load()
	c.mu.Lock()
	if err := refresh(); err != nil {
		c.mu.Unlock()
		handler(Event[T]{Previous: previous, Err: err})
		return
	}
	current, err := c.decode(ctx)
	changed := err == nil && !reflect.DeepEqual(previous, current)
	if changed {
		c.current.Store(current)
	}
	c.mu.Unlock()

	if err != nil {
		handler(Event[T]{Previous: previous, Err: err})
	} else if changed {
		handler(Event[T]{Previous: previous, Current: current})
	}
}
