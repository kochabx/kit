package cx

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// State identifies the current Container lifecycle phase.
type State uint8

const (
	StateNew State = iota
	StateStarting
	StateRunning
	StateStopping
	StateStopped
	StateFailed
	StateStopFailed
)

func (s State) String() string {
	names := [...]string{"new", "starting", "running", "stopping", "stopped", "failed", "stop_failed"}
	if int(s) >= len(names) {
		return "unknown"
	}
	return names[s]
}

type provider struct {
	name         string
	constructor  func(*Container) (any, error)
	value        any
	built        bool
	owned        bool
	declaredDeps []string
	deps         []string
}

type Container struct {
	mu         sync.RWMutex
	providers  map[string]*provider
	keys       []string
	buildOrder []string
	buildStack []string
	state      State

	shutdownTimeout      time.Duration
	componentStopTimeout time.Duration
	rollbackTimeout      time.Duration
	healthTimeout        time.Duration

	onStart    []func(context.Context) error
	onStarted  []func(context.Context) error
	onStopping []func(context.Context) error
	onStop     []func(context.Context) error

	onStoppingDone []bool
	onStopDone     []bool
	supervisor     *supervisor
	healthFlights  map[string]*healthFlight
}

func New(opts ...Option) *Container {
	c := &Container{
		providers:            make(map[string]*provider),
		shutdownTimeout:      30 * time.Second,
		componentStopTimeout: 10 * time.Second,
		rollbackTimeout:      30 * time.Second,
		healthTimeout:        10 * time.Second,
		healthFlights:        make(map[string]*healthFlight),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func Provide[T any](
	c *Container,
	key Key[T],
	constructor func(*Container) (T, error),
	options ...ProvideOption,
) error {
	if c == nil {
		return ErrNilContainer
	}
	if key.name == "" {
		return ErrInvalidKey
	}
	if constructor == nil {
		return fmt.Errorf("%w: %s", ErrNilConstructor, key.name)
	}

	p := &provider{
		name: key.name,
		constructor: func(c *Container) (any, error) {
			return constructor(c)
		},
	}
	for _, option := range options {
		if option != nil {
			if err := option(p); err != nil {
				return fmt.Errorf("configure %s: %w", key.name, err)
			}
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.idleLocked() {
		return fmt.Errorf("%w: %s", ErrContainerNotIdle, c.state)
	}
	if _, exists := c.providers[key.name]; exists {
		return fmt.Errorf("%w: %s", ErrComponentExists, key.name)
	}
	c.providers[key.name] = p
	c.keys = append(c.keys, key.name)
	return nil
}

func MustProvide[T any](
	c *Container,
	key Key[T],
	constructor func(*Container) (T, error),
	options ...ProvideOption,
) {
	if err := Provide(c, key, constructor, options...); err != nil {
		panic(err)
	}
}

func Supply[T any](c *Container, key Key[T], value T, options ...ProvideOption) error {
	return Provide(c, key, func(*Container) (T, error) {
		return value, nil
	}, options...)
}

func MustSupply[T any](c *Container, key Key[T], value T, options ...ProvideOption) {
	if err := Supply(c, key, value, options...); err != nil {
		panic(err)
	}
}

func Get[T any](c *Container, key Key[T]) (T, error) {
	var zero T
	if c == nil {
		return zero, ErrNilContainer
	}

	c.mu.RLock()
	p, exists := c.providers[key.name]
	state := c.state
	if exists && p.built {
		value := p.value
		c.mu.RUnlock()
		result, ok := value.(T)
		if !ok {
			return zero, fmt.Errorf("%w: %s", ErrTypeMismatch, key.name)
		}
		return result, nil
	}
	c.mu.RUnlock()

	if !exists {
		return zero, fmt.Errorf("%w: %s", ErrComponentNotFound, key.name)
	}
	if state != StateStarting {
		return zero, fmt.Errorf("%w: %s is not built in %s", ErrComponentNotFound, key.name, state)
	}
	if err := c.build(key.name); err != nil {
		return zero, err
	}

	c.mu.RLock()
	value := p.value
	c.mu.RUnlock()
	result, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrTypeMismatch, key.name)
	}
	return result, nil
}

func MustGet[T any](c *Container, key Key[T]) T {
	value, err := Get(c, key)
	if err != nil {
		panic(err)
	}
	return value
}

func Has[T any](c *Container, key Key[T]) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.providers[key.name]
	return exists
}

func (c *Container) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.keys)
}

func (c *Container) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.providers)
}

func (c *Container) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Container) DependencyGraph() map[string][]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	graph := make(map[string][]string, len(c.providers))
	for name, p := range c.providers {
		graph[name] = slices.Clone(p.deps)
	}
	return graph
}

func (c *Container) idleLocked() bool {
	return c.state == StateNew || c.state == StateStopped || c.state == StateFailed
}

func (c *Container) resetInstancesLocked() {
	for _, p := range c.providers {
		p.value = nil
		p.built = false
		p.owned = false
		p.deps = nil
	}
	c.buildOrder = nil
	c.buildStack = nil
	c.onStoppingDone = make([]bool, len(c.onStopping))
	c.onStopDone = make([]bool, len(c.onStop))
	c.healthFlights = make(map[string]*healthFlight)
}
