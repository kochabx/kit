// Package cx provides a lightweight, strongly typed dependency-injection container
// with topological startup/shutdown ordering, rollback, health checks, and supervision.
package cx

import (
	"context"
	"errors"
	"reflect"
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

// Starter initializes a component during container startup.
type Starter interface {
	Start(context.Context) error
}

// Stopper releases a component during container shutdown.
type Stopper interface {
	Stop(context.Context) error
}

// HealthChecker reports a component's current health.
type HealthChecker interface {
	HealthCheck(context.Context) error
}

// Runnable represents a long-running, context-controlled component.
type Runnable interface {
	Run(context.Context) error
}

// Supervised exposes completion and failure of a long-running component.
type Supervised interface {
	Done() <-chan struct{}
	Err() error
}

// Dependency identifies a component that another component depends on.
type Dependency interface {
	dependencyName() string
}

// Key is a typed component identity.
type Key[T any] struct {
	name string
}

// NewKey creates a typed component key with an explicit name.
func NewKey[T any](name string) Key[T] {
	return Key[T]{name: name}
}

// TypeKey creates an unnamed singleton key derived from T's type name.
func TypeKey[T any]() Key[T] {
	return Key[T]{name: reflect.TypeFor[T]().String()}
}

func (k Key[T]) String() string         { return k.name }
func (k Key[T]) dependencyName() string { return k.name }

// ContainerMetrics summarizes current container state.
type ContainerMetrics struct {
	ComponentCount int
	State          State
}

var (
	ErrComponentNotFound    = errors.New("component not found")
	ErrComponentExists      = errors.New("component already exists")
	ErrCircularDependency   = errors.New("circular dependency")
	ErrInvalidDependency    = errors.New("invalid dependency")
	ErrTypeMismatch         = errors.New("type mismatch")
	ErrContainerNotIdle     = errors.New("container is not idle")
	ErrInvalidKey           = errors.New("invalid key")
	ErrNilContainer         = errors.New("nil container")
	ErrNilConstructor       = errors.New("nil constructor")
	ErrNilContext           = errors.New("nil context")
	ErrInvalidState         = errors.New("invalid container state")
	ErrConstructorPanic     = errors.New("constructor panic")
	ErrComponentExited      = errors.New("component exited unexpectedly")
	ErrNilRunnable          = errors.New("nil runnable")
	ErrRunnerAlreadyStarted = errors.New("runner already started")
	ErrRunnerNotRunning     = errors.New("runner is not running")
	ErrRunnerExited         = errors.New("runner exited")
	ErrRunnerPanic          = errors.New("runner panic")
)
