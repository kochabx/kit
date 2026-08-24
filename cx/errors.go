package cx

import "errors"

var (
	// ErrComponentNotFound indicates that a key is not registered or built.
	ErrComponentNotFound = errors.New("component not found")
	// ErrComponentExists indicates duplicate registration of a key name.
	ErrComponentExists = errors.New("component already exists")
	// ErrCircularDependency indicates a cycle in the dependency graph.
	ErrCircularDependency = errors.New("circular dependency")
	// ErrInvalidDependency indicates an empty or nil explicit dependency.
	ErrInvalidDependency = errors.New("invalid dependency")
	// ErrTypeMismatch indicates an internal key/value type invariant violation.
	ErrTypeMismatch = errors.New("type mismatch")
	// ErrContainerNotIdle indicates registration during an active lifecycle.
	ErrContainerNotIdle = errors.New("container is not idle")
	// ErrInvalidKey indicates an empty component key.
	ErrInvalidKey = errors.New("invalid key")
	// ErrNilContainer indicates a nil Container argument.
	ErrNilContainer = errors.New("nil container")
	// ErrNilConstructor indicates a nil component constructor.
	ErrNilConstructor = errors.New("nil constructor")
	// ErrNilContext indicates a nil context.Context argument.
	ErrNilContext = errors.New("nil context")
	// ErrInvalidState indicates an invalid lifecycle operation for the current state.
	ErrInvalidState = errors.New("invalid container state")
	// ErrConstructorPanic wraps a panic raised by a component constructor.
	ErrConstructorPanic = errors.New("constructor panic")
	// ErrComponentExited indicates a supervised component exited without an error.
	ErrComponentExited = errors.New("component exited unexpectedly")
	// ErrNilRunnable indicates a nil or typed-nil Runnable.
	ErrNilRunnable = errors.New("nil runnable")
	// ErrRunnerAlreadyStarted indicates Start was called for an active Runner.
	ErrRunnerAlreadyStarted = errors.New("runner already started")
	// ErrRunnerNotRunning indicates a Runner has no active execution.
	ErrRunnerNotRunning = errors.New("runner is not running")
	// ErrRunnerExited indicates a Runner exited before shutdown.
	ErrRunnerExited = errors.New("runner exited")
	// ErrRunnerPanic wraps a panic raised by Runnable.Run.
	ErrRunnerPanic = errors.New("runner panic")
)
