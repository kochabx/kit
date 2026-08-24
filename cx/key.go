package cx

// Dependency identifies a component that another component depends on.
type Dependency interface {
	dependencyName() string
}

// Key is a typed component identity.
type Key[T any] struct {
	name string
}

// NewKey creates a typed component key.
func NewKey[T any](name string) Key[T] {
	return Key[T]{name: name}
}

func (k Key[T]) String() string {
	return k.name
}

func (k Key[T]) dependencyName() string {
	return k.name
}
