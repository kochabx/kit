package defaults

import (
	"fmt"
	"reflect"
	"sync"
)

// Applier is immutable. Custom decoders and filters must be safe for concurrent use.
type Applier struct {
	config config
}

var sharedApplier = sync.OnceValue(func() *Applier {
	applier, err := New()
	if err != nil {
		panic(err)
	}
	return applier
})

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
		c.decoder = newBuiltinDecoder(c.timeLayouts)
	}
	return &Applier{config: c}, nil
}

// Apply applies defaults atomically. The target remains unchanged on error.
func Apply(target any, options ...Option) error {
	if len(options) == 0 {
		return sharedApplier().Apply(target)
	}
	applier, err := New(options...)
	if err != nil {
		return err
	}
	return applier.Apply(target)
}

// Apply applies defaults atomically for configuration data. Concurrent calls
// must use distinct targets and concurrency-safe custom callbacks.
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

	working, err := deepClone(value.Elem(), a.config.maxDepth)
	if err != nil {
		return err
	}
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
