package tag

import (
	"fmt"
	"reflect"
)

// Error types for tag processing
var (
	ErrTargetMustBePointer = fmt.Errorf("target must be a pointer")
	ErrTargetIsNil         = fmt.Errorf("target is nil")
	ErrUnsupportedType     = fmt.Errorf("unsupported type")
	ErrMaxDepthExceeded    = fmt.Errorf("max recursion depth exceeded")
	ErrInvalidTagValue     = fmt.Errorf("invalid tag value")
)

// FieldError wraps an error with field path context
type FieldError struct {
	Path  string
	Kind  reflect.Kind
	Tag   string
	Value string
	Err   error
}

// Error implements error interface
func (e *FieldError) Error() string {
	return fmt.Sprintf("field %q (type: %s, tag: %q, value: %q): %v",
		e.Path, e.Kind, e.Tag, e.Value, e.Err)
}

// Unwrap returns the wrapped error
func (e *FieldError) Unwrap() error {
	return e.Err
}

// newFieldError creates a new field error with context
func newFieldError(path string, kind reflect.Kind, tag, value string, err error) error {
	return &FieldError{
		Path:  path,
		Kind:  kind,
		Tag:   tag,
		Value: value,
		Err:   err,
	}
}
