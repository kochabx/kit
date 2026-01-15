package ioc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kochabx/kit/log"
)

// Lifecycle manages the lifecycle of components in the IoC container.
type Lifecycle struct {
	store              *Store
	state              LifecycleState
	mu                 sync.RWMutex
	dependencyInjector *SimpleDependencyInjector
	componentStates    map[string]ComponentState
	shutdownHandlers   []func() error
	shutdownTimeout    time.Duration
}

// LifecycleState represents the current state of the container.
type LifecycleState int

const (
	// StateNew indicates the container is newly created but not initialized.
	StateNew LifecycleState = iota
	// StateInitializing indicates the container is currently being initialized.
	StateInitializing
	// StateRunning indicates the container is fully initialized and running.
	StateRunning
	// StateShutting indicates the container is currently shutting down.
	StateShutting
	// StateStopped indicates the container has been shut down.
	StateStopped
	// StateError indicates the container is in an error state.
	StateError
)

// ComponentState represents the current state of a component.
type ComponentState int

const (
	// ComponentStateNew indicates the component is newly registered but not initialized.
	ComponentStateNew ComponentState = iota
	// ComponentStateInitializing indicates the component is currently being initialized.
	ComponentStateInitializing
	// ComponentStateRunning indicates the component is fully initialized and running.
	ComponentStateRunning
	// ComponentStateShutting indicates the component is currently shutting down.
	ComponentStateShutting
	// ComponentStateStopped indicates the component has been shut down.
	ComponentStateStopped
	// ComponentStateError indicates the component is in an error state.
	ComponentStateError
)

// String returns a string representation of the lifecycle state.
func (s LifecycleState) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateInitializing:
		return "initializing"
	case StateRunning:
		return "running"
	case StateShutting:
		return "shutting"
	case StateStopped:
		return "stopped"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// NewLifecycle creates a new lifecycle manager.
func NewLifecycle(store *Store, shutdownTimeout time.Duration) *Lifecycle {
	if shutdownTimeout == 0 {
		shutdownTimeout = 30 * time.Second
	}

	return &Lifecycle{
		store:              store,
		dependencyInjector: NewSimpleDependencyInjector(store),
		state:              StateNew,
		shutdownTimeout:    shutdownTimeout,
		componentStates:    make(map[string]ComponentState),
		shutdownHandlers:   make([]func() error, 0),
	}
}

// logf logs a message if verbose logging is enabled.
func (lm *Lifecycle) logf(format string, args ...any) {
	if lm.store.verbose {
		log.Infof(format, args...)
	}
}

// GetState returns the current lifecycle state.
func (lm *Lifecycle) GetState() LifecycleState {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.state
}

// setState sets the lifecycle state.
func (lm *Lifecycle) setState(state LifecycleState) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	oldState := lm.state
	lm.state = state
	lm.logf("[lifecycle] state changed: %s -> %s", oldState, state)
}

// Initialize initializes the container and all its components.
func (lm *Lifecycle) Initialize(ctx context.Context) error {
	// Check current state
	if !lm.canTransitionTo(StateInitializing) {
		return fmt.Errorf("cannot initialize container in state %s", lm.GetState())
	}

	lm.setState(StateInitializing)

	// Create context with timeout if not set
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Initialize components in phases
	phases := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"validate", lm.validateComponents},
		{"resolve_dependencies", lm.resolveDependencies},
		{"initialize_components", lm.initializeComponents},
		{"post_initialize", lm.postInitialize},
	}

	for _, phase := range phases {
		lm.logf("[lifecycle] starting phase: %s", phase.name)

		select {
		case <-ctx.Done():
			lm.setState(StateError)
			return fmt.Errorf("initialization cancelled during phase %s: %w", phase.name, ctx.Err())
		default:
			if err := phase.fn(ctx); err != nil {
				lm.setState(StateError)
				return fmt.Errorf("initialization failed in phase %s: %w", phase.name, err)
			}
		}

		lm.logf("[lifecycle] completed phase: %s", phase.name)
	}

	lm.setState(StateRunning)
	lm.logf("[lifecycle] container initialized successfully")
	return nil
}

// validateComponents validates all registered components.
func (lm *Lifecycle) validateComponents(ctx context.Context) error {
	lm.store.mu.RLock()
	namespaces := make([]*Namespace, 0, len(lm.store.namespaces))
	for _, ns := range lm.store.namespaces {
		namespaces = append(namespaces, ns)
	}
	lm.store.mu.RUnlock()

	for _, ns := range namespaces {
		ns.mu.RLock()
		components := make([]*ComponentDescriptor, 0, len(ns.components))
		for _, descriptor := range ns.components {
			components = append(components, descriptor)
		}
		ns.mu.RUnlock()

		for _, descriptor := range components {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				if err := ValidateComponent(descriptor.Instance); err != nil {
					return fmt.Errorf("validation failed for component %s in namespace %s: %w",
						descriptor.Name, ns.name, err)
				}
			}
		}
	}

	return nil
}

// resolveDependencies resolves and injects dependencies.
func (lm *Lifecycle) resolveDependencies(ctx context.Context) error {
	return lm.dependencyInjector.ResolveDependencies(ctx)
}

// initializeComponents initializes all components.
func (lm *Lifecycle) initializeComponents(ctx context.Context) error {
	return lm.store.InitWithContext(ctx)
}

// postInitialize performs post-initialization tasks.
func (lm *Lifecycle) postInitialize(ctx context.Context) error {
	// Execute any post-initialization hooks
	if lm.store.hooks != nil {
		for _, hook := range lm.store.hooks.AfterInit {
			if err := hook(ctx, lm.store); err != nil {
				return fmt.Errorf("post-init hook failed: %w", err)
			}
		}
	}

	// Perform initial health check
	if err := lm.store.HealthCheck(ctx); err != nil {
		log.Warnf("[lifecycle] initial health check failed: %v", err)
		// Don't fail initialization for health check failures
	}

	return nil
}

// Shutdown gracefully shuts down the container.
func (lm *Lifecycle) Shutdown(ctx context.Context) error {
	// Check current state
	if !lm.canTransitionTo(StateShutting) {
		return fmt.Errorf("cannot shutdown container in state %s", lm.GetState())
	}

	lm.setState(StateShutting)

	// Create context with timeout if not set
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lm.shutdownTimeout)
		defer cancel()
	}

	// Shutdown in phases
	phases := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"pre_shutdown", lm.preShutdown},
		{"shutdown_components", lm.shutdownComponents},
		{"post_shutdown", lm.postShutdown},
	}

	var shutdownErrors []error

	for _, phase := range phases {
		lm.logf("[lifecycle] starting shutdown phase: %s", phase.name)

		select {
		case <-ctx.Done():
			shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown cancelled during phase %s: %w", phase.name, ctx.Err()))
			goto shutdown_complete
		default:
			if err := phase.fn(ctx); err != nil {
				shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown failed in phase %s: %w", phase.name, err))
				// Continue with other phases even if one fails
			}
		}

		lm.logf("[lifecycle] completed shutdown phase: %s", phase.name)
	}

shutdown_complete:

	if len(shutdownErrors) > 0 {
		lm.setState(StateError)
		return fmt.Errorf("shutdown completed with errors: %v", shutdownErrors)
	}

	lm.setState(StateStopped)
	lm.logf("[lifecycle] container shutdown successfully")
	return nil
}

// preShutdown performs pre-shutdown tasks.
func (lm *Lifecycle) preShutdown(ctx context.Context) error {
	// Execute pre-shutdown hooks
	if lm.store.hooks != nil {
		for _, hook := range lm.store.hooks.BeforeDestroy {
			if err := hook(ctx, lm.store); err != nil {
				return fmt.Errorf("pre-shutdown hook failed: %w", err)
			}
		}
	}

	return nil
}

// shutdownComponents shuts down all components.
func (lm *Lifecycle) shutdownComponents(ctx context.Context) error {
	return lm.store.Shutdown(ctx)
}

// postShutdown performs post-shutdown tasks.
func (lm *Lifecycle) postShutdown(ctx context.Context) error {
	// Execute post-shutdown hooks
	if lm.store.hooks != nil {
		for _, hook := range lm.store.hooks.AfterDestroy {
			if err := hook(ctx, lm.store); err != nil {
				return fmt.Errorf("post-shutdown hook failed: %w", err)
			}
		}
	}

	return nil
}

// canTransitionTo checks if the container can transition to the given state.
func (lm *Lifecycle) canTransitionTo(target LifecycleState) bool {
	current := lm.GetState()

	switch target {
	case StateInitializing:
		return current == StateNew
	case StateRunning:
		return current == StateInitializing
	case StateShutting:
		return current == StateRunning || current == StateError
	case StateStopped:
		return current == StateShutting
	case StateError:
		return true // Can transition to error from any state
	default:
		return false
	}
}

// Restart restarts the container.
func (lm *Lifecycle) Restart(ctx context.Context) error {
	// Shutdown first if running
	if lm.GetState() == StateRunning {
		if err := lm.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown during restart: %w", err)
		}
	}

	// Reset to new state
	lm.setState(StateNew)

	// Initialize again
	return lm.Initialize(ctx)
}

// HealthCheck performs a comprehensive health check.
func (lm *Lifecycle) HealthCheck(ctx context.Context) *HealthReport {
	report := &HealthReport{
		Timestamp:  time.Now(),
		State:      lm.GetState(),
		Healthy:    true,
		Components: make(map[string]ComponentHealth),
	}

	// Check overall state
	if report.State != StateRunning {
		report.Healthy = false
		report.Message = fmt.Sprintf("container not in running state: %s", report.State)
		return report
	}

	// Check individual components
	lm.store.mu.RLock()
	namespaces := make([]*Namespace, 0, len(lm.store.namespaces))
	for _, ns := range lm.store.namespaces {
		namespaces = append(namespaces, ns)
	}
	lm.store.mu.RUnlock()

	for _, ns := range namespaces {
		ns.mu.RLock()
		components := make([]*ComponentDescriptor, 0, len(ns.components))
		for _, descriptor := range ns.components {
			components = append(components, descriptor)
		}
		ns.mu.RUnlock()

		for _, descriptor := range components {
			componentHealth := ComponentHealth{
				Name:      descriptor.Name,
				Namespace: descriptor.Namespace,
				Healthy:   true,
			}

			if checker, ok := descriptor.Instance.(HealthChecker); ok {
				if err := checker.HealthCheck(ctx); err != nil {
					componentHealth.Healthy = false
					componentHealth.Error = err.Error()
					report.Healthy = false
				}
			}

			report.Components[descriptor.Name] = componentHealth
		}
	}

	if report.Healthy {
		report.Message = "all components healthy"
	} else {
		report.Message = "some components unhealthy"
	}

	return report
}

// HealthReport represents a health check report.
type HealthReport struct {
	Timestamp  time.Time                  `json:"timestamp"`
	State      LifecycleState             `json:"state"`
	Healthy    bool                       `json:"healthy"`
	Message    string                     `json:"message"`
	Components map[string]ComponentHealth `json:"components"`
}

// ComponentHealth represents the health status of a single component.
type ComponentHealth struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Healthy   bool   `json:"healthy"`
	Error     string `json:"error,omitempty"`
}

// GetMetrics returns container metrics.
func (lm *Lifecycle) GetMetrics(ctx context.Context) *ContainerMetrics {
	metrics := &ContainerMetrics{
		Timestamp:       time.Now(),
		State:           lm.GetState(),
		TotalComponents: lm.store.Count(),
		Namespaces:      make(map[string]int),
		ComponentTypes:  make(map[string]int),
	}

	// Collect namespace metrics
	lm.store.mu.RLock()
	for name, ns := range lm.store.namespaces {
		ns.mu.RLock()
		metrics.Namespaces[name] = len(ns.components)

		// Collect component metrics by name
		for _, descriptor := range ns.components {
			componentName := descriptor.Name
			metrics.ComponentTypes[componentName]++
		}
		ns.mu.RUnlock()
	}
	lm.store.mu.RUnlock()

	return metrics
}

// ContainerMetrics represents container metrics.
type ContainerMetrics struct {
	Timestamp       time.Time      `json:"timestamp"`
	State           LifecycleState `json:"state"`
	TotalComponents int            `json:"total_components"`
	Namespaces      map[string]int `json:"namespaces"`
	ComponentTypes  map[string]int `json:"component_types"`
}
