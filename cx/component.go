package cx

import "context"

// Starter is implemented by values that require initialization.
// Start is called during Container.Start in dependency order.
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is implemented by values that require cleanup on shutdown.
// Stop is called during Container.Stop in reverse dependency order.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Checker is implemented by values that can report their health status.
type Checker interface {
	Check(ctx context.Context) error
}
