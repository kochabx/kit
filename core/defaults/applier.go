package defaults

import (
	"fmt"
	"reflect"
)

// Applier is an immutable, concurrency-safe default-value processor.
type Applier struct {
	config config
}

func New(options ...Option) (*Applier, error) {
	c := defaultConfig()
	for index, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := option(&c); err != nil {
			return nil, err
		}
	}
	if c.decoder == nil {
		c.decoder = builtinDecoder{timeLayouts: c.timeLayouts}
	}
	return &Applier{config: c}, nil
}

// Apply applies defaults atomically. The target remains unchanged on error.
func Apply(target any, options ...Option) error {
	applier, err := New(options...)
	if err != nil {
		return err
	}
	return applier.Apply(target)
}

// Apply applies defaults atomically. Applier may be shared by goroutines.
func (a *Applier) Apply(target any) (err error) {
	if a == nil {
		return fmt.Errorf("%w: nil applier", ErrInvalidOption)
	}
	value := reflect.ValueOf(target)
	if !value.IsValid() || value.Kind() != reflect.Pointer {
		return ErrTargetMustBePointer
	}
	if value.IsNil() {
		return ErrTargetIsNil
	}
	if value.Elem().Kind() != reflect.Struct {
		return ErrTargetMustBePointer
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrApplyPanic, recovered)
		}
	}()

	working := deepClone(value.Elem())
	state := traversal{
		applier: a,
		visited: make(map[visit]struct{}),
		types:   make(map[reflect.Type]int),
	}
	if err := state.applyStruct(working, "", 0); err != nil {
		return err
	}
	value.Elem().Set(working)
	return nil
}
