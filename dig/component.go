package dig

import "context"

// Component is the base interface that all managed components must implement.
// The name must be unique within its group.
type Component interface {
	Name() string
}

// Starter is implemented by components that require initialization before use.
// Start is called during Container.Start() in group priority order.
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is implemented by components that require cleanup on shutdown.
// Stop is called during Container.Stop() in reverse group priority order.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Checker is implemented by components that can report their health status.
type Checker interface {
	Check(ctx context.Context) error
}

// Orderable is implemented by components that declare an explicit initialization order.
// Lower values are initialized first within the same group.
type Orderable interface {
	Order() int
}

// Provider is implemented by components that expose a named dependency
// for injection into Consumer components.
type Provider interface {
	Component
	// ProvidesDependency returns the dependency key this component provides.
	ProvidesDependency() string
	// GetDependency returns the dependency value.
	GetDependency() any
}

// Consumer is implemented by components that require dependencies from Provider components.
// Dependencies are resolved and injected automatically during Container.Start().
type Consumer interface {
	Component
	// RequiredDependencies returns the dependency keys this component needs.
	RequiredDependencies() []string
	// SetDependency is called once per required dependency with the resolved value.
	SetDependency(key string, dep any) error
}
