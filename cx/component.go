package cx

import "context"

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
