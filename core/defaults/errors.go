package defaults

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrTargetMustBePointer = errors.New("target must be a pointer to a struct")
	ErrTargetIsNil         = errors.New("target is nil")
	ErrUnsupportedType     = errors.New("unsupported default type")
	ErrInvalidTagValue     = errors.New("invalid default value")
	ErrInvalidOption       = errors.New("invalid defaults option")
	ErrMaxDepthExceeded    = errors.New("maximum traversal depth exceeded")
	ErrApplyPanic          = errors.New("panic while applying defaults")
)

// FieldError identifies a default that could not be applied. Error omits the
// raw tag value because defaults may contain credentials.
type FieldError struct {
	Path string
	Type reflect.Type
	Tag  string
	Err  error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("defaults: invalid value for field %q (%s), tag %q", e.Path, e.Type, e.Tag)
}

func (e *FieldError) Unwrap() error {
	return e.Err
}

func fieldError(path string, typ reflect.Type, tag string, err error) error {
	return &FieldError{
		Path: path,
		Type: typ,
		Tag:  tag,
		Err:  errors.Join(ErrInvalidTagValue, err),
	}
}
