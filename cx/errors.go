package cx

import "errors"

var (
	// ErrComponentNotFound is returned when a key is not registered in the container.
	ErrComponentNotFound = errors.New("component not found")

	// ErrComponentExists is returned when a key is already registered.
	ErrComponentExists = errors.New("component already exists")

	// ErrCircularDependency is returned when a circular dependency is detected
	// during the build phase.
	ErrCircularDependency = errors.New("circular dependency")

	// ErrTypeMismatch is returned when a Get type assertion fails.
	ErrTypeMismatch = errors.New("type mismatch")

	// ErrContainerNotIdle is returned when Provide/Supply is called outside
	// StateNew or StateStopped.
	ErrContainerNotIdle = errors.New("container is not idle")

	// ErrInvalidKey is returned when an empty key is provided.
	ErrInvalidKey = errors.New("invalid key")
)
