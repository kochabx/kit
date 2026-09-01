package config

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const fileReloadDelay = 50 * time.Millisecond

type debouncer struct {
	mu         sync.Mutex
	timer      *time.Timer
	generation uint64
}

func (d *debouncer) schedule(delay time.Duration, callback func()) {
	d.mu.Lock()
	d.generation++
	generation := d.generation
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(delay, func() {
		d.mu.Lock()
		if generation != d.generation {
			d.mu.Unlock()
			return
		}
		d.timer = nil
		d.mu.Unlock()
		callback()
	})
	d.mu.Unlock()
}

// Watch starts Viper's native file watcher. Load must succeed first. Viper
// does not provide a way to stop the watcher, so it lives for the process
// lifetime.
func (c *Config[T]) Watch(handler func(Event[T])) error {
	if err := c.startWatch(SourceFile, handler); err != nil {
		return err
	}

	var debounce debouncer
	c.options.viper.OnConfigChange(func(fsnotify.Event) {
		// Editors commonly emit several events for one logical save. Reload only
		// after the event stream has been quiet long enough for the write to finish.
		debounce.schedule(fileReloadDelay, func() {
			c.reload(context.Background(), c.expandEnvironment, handler)
		})
	})
	c.options.viper.WatchConfig()
	return nil
}

// WatchRemote polls a remote source until ctx is canceled. Load must succeed
// first. The context must not be nil.
func (c *Config[T]) WatchRemote(ctx context.Context, handler func(Event[T])) error {
	if err := c.startWatch(SourceRemote, handler); err != nil {
		return err
	}

	go func() {
		defer c.stopWatching()
		ticker := time.NewTicker(c.options.remote.Interval)
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
	c.mu.Lock()
	defer c.mu.Unlock()
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
	c.mu.Lock()
	previous := c.current.Load()
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
