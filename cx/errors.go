package cx

import "errors"

var (
	// ErrGroupNotFound is returned when a group is not found.
	ErrGroupNotFound = errors.New("group not found")

	// ErrComponentNotFound is returned when a component is not found.
	ErrComponentNotFound = errors.New("component not found")

	// ErrComponentAlreadyExists is returned when a component with the same name is already registered.
	ErrComponentAlreadyExists = errors.New("component already exists")

	// ErrGroupAlreadyExists is returned when a group with the same name is already registered.
	ErrGroupAlreadyExists = errors.New("group already exists")

	// ErrInvalidComponent is returned when a component is nil or has an empty name.
	ErrInvalidComponent = errors.New("invalid component")

	// ErrDependencyNotFound is returned when a required dependency cannot be resolved.
	ErrDependencyNotFound = errors.New("dependency not found")

	// ErrCircularDependency is returned when a circular dependency is detected.
	ErrCircularDependency = errors.New("circular dependency detected")

	// ErrNotRunning is returned when an operation requires the container to be running.
	ErrNotRunning = errors.New("container is not running")

	// ErrAlreadyStarted is returned when Start is called on an already-started container.
	ErrAlreadyStarted = errors.New("container already started")

	// ErrDuplicateProvider is returned when two providers advertise the same dependency key.
	ErrDuplicateProvider = errors.New("duplicate dependency provider")

	// ErrNotRegisterable is returned when Provide is called in a non-registerable state.
	ErrNotRegisterable = errors.New("container is not in a registerable state")
)
