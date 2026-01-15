package ioc

import (
	"context"

	"github.com/gin-gonic/gin"
)

// Component defines the basic contract for all IoC components.
// This interface follows the principle of small, focused interfaces.
type Component interface {
	// Name returns the unique identifier for this component.
	// It should be immutable and unique within its namespace.
	Name() string
}

// Initializer defines components that require initialization.
// This is separated from Component to follow the Interface Segregation Principle.
type Initializer interface {
	// Init initializes the component with context support for cancellation and timeout.
	// It should be idempotent and safe to call multiple times.
	Init(ctx context.Context) error
}

// Destroyer defines components that require cleanup during shutdown.
// This enables proper resource management and graceful shutdowns.
type Destroyer interface {
	// Destroy cleans up resources used by the component.
	// It should be idempotent and safe to call multiple times.
	Destroy(ctx context.Context) error
}

// HealthChecker defines components that can report their health status.
// This is useful for monitoring and health check endpoints.
type HealthChecker interface {
	// HealthCheck returns the current health status of the component.
	// It should be lightweight and non-blocking.
	HealthCheck(ctx context.Context) error
}

// Orderable defines components that have initialization/execution priority.
// Lower values indicate higher priority (executed first).
type Orderable interface {
	// Order returns the order value for this component.
	// Components with lower order values are processed first.
	Order() int
}

// DependencyInjector defines a component that can resolve and inject dependencies.
// This replaces reflection-based dependency injection with explicit name registration.
type DependencyInjector interface {
	// RegisterDependency registers a dependency provider for a specific component name.
	RegisterDependency(name string, provider any) error

	// ResolveDependency resolves a dependency by its component name.
	ResolveDependency(name string) (any, error)

	// InjectInto injects dependencies into a component.
	InjectInto(component Component) error
}

// DependencyProvider defines components that can provide dependencies to other components.
type DependencyProvider interface {
	Component
	// ProvidesDependency returns the component name this provider serves.
	ProvidesDependency() string

	// GetDependency returns the dependency instance.
	GetDependency() any
}

// DependencyConsumer defines components that require dependencies from other components.
type DependencyConsumer interface {
	Component
	// RequiredDependencies returns the component names of dependencies this component requires.
	RequiredDependencies() []string

	// SetDependency sets a dependency by its component name.
	SetDependency(name string, dependency any) error
}

// BaseComponent represents a basic managed component.
type BaseComponent interface {
	Component
	Initializer
}

// Repository represents a data access component.
type Repository interface {
	BaseComponent
	// Close closes the repository and releases resources.
	Close(ctx context.Context) error
}

// HTTPRouter defines components that register HTTP routes.
// This interface is more generic and not tied to Gin specifically.
type HTTPRouter interface {
	Component
	// RegisterRoutes registers HTTP routes with the given router.
	RegisterRoutes(router any)
}

// GinRouter defines Gin-specific router registration.
// This is the preferred interface for Gin route registration.
type GinRouter interface {
	Component
	// RegisterGinRoutes registers routes with a Gin router.
	RegisterGinRoutes(r gin.IRouter)
}
