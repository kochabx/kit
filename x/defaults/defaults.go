// Package defaults populates zero-valued struct fields from struct tags
// with pre-compiled plans, strict stdlib/JSON v2 decoding, and atomic Undo Log rollback.
package defaults

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"
)

// Setter can be implemented by target structs to apply dynamic cross-field defaults
// after tag defaults succeed.
type Setter interface {
	SetDefaults()
}

type config struct {
	tagName     string
	maxDepth    int
	filter      func(reflect.StructField) bool
	decoders    map[reflect.Type]customDecoderFunc
	timeLayouts []string
}

// Option customizes Applier behavior.
type Option func(*config) error

// WithTag overrides the struct tag key (default: "default").
func WithTag(tag string) Option {
	return func(c *config) error {
		if tag == "" {
			return fmt.Errorf("%w: empty tag name", ErrInvalidOption)
		}
		c.tagName = tag
		return nil
	}
}

// WithMaxDepth limits nested traversal depth (default: 64).
func WithMaxDepth(maxDepth int) Option {
	return func(c *config) error {
		if maxDepth <= 0 {
			return fmt.Errorf("%w: maxDepth must be positive", ErrInvalidOption)
		}
		c.maxDepth = maxDepth
		return nil
	}
}

// WithFieldFilter skips fields for which fn returns false.
func WithFieldFilter(fn func(reflect.StructField) bool) Option {
	return func(c *config) error {
		if fn == nil {
			return fmt.Errorf("%w: nil field filter", ErrInvalidOption)
		}
		c.filter = fn
		return nil
	}
}

// WithDecoder registers a custom parser for typ, overriding built-in decoding.
func WithDecoder(typ reflect.Type, fn func(string) (any, error)) Option {
	return func(c *config) error {
		if typ == nil || fn == nil {
			return fmt.Errorf("%w: nil type or decoder function", ErrInvalidOption)
		}
		c.decoders[typ] = fn
		return nil
	}
}

// WithTimeLayouts sets accepted layouts for time.Time defaults.
func WithTimeLayouts(layouts ...string) Option {
	return func(c *config) error {
		if len(layouts) == 0 {
			return fmt.Errorf("%w: at least one time layout is required", ErrInvalidOption)
		}
		if slices.Contains(layouts, "") {
			return fmt.Errorf("%w: empty time layout", ErrInvalidOption)
		}
		c.timeLayouts = slices.Clone(layouts)
		return nil
	}
}

// Applier applies configured tag defaults atomically using an Undo Log.
type Applier struct {
	cfg     config
	decoder *builtinDecoder
}

var defaultApplier = mustResolveApplier()

func mustResolveApplier(opts ...Option) *Applier {
	a, err := NewApplier(opts...)
	if err != nil {
		panic(err)
	}
	return a
}

// NewApplier constructs a reusable Applier with the given options.
func NewApplier(opts ...Option) (*Applier, error) {
	cfg := config{
		tagName:     "default",
		maxDepth:    64,
		decoders:    make(map[reflect.Type]customDecoderFunc),
		timeLayouts: []string{time.RFC3339Nano, time.RFC3339, time.DateOnly},
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidOption)
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	return &Applier{
		cfg:     cfg,
		decoder: newBuiltinDecoder(cfg.timeLayouts, cfg.decoders),
	}, nil
}

// Apply populates zero-valued fields on *dst atomically for tag-driven defaults.
// On any error or panic, all tag-applied mutations are reverted in reverse order
// via an Undo Log. Direct field mutations performed inside custom
// Setter.SetDefaults implementations run outside the Undo Log (preserving
// support for structs containing non-copyable fields such as sync.Mutex).
func (a *Applier) Apply[T any](dst *T) (err error) {
	if a == nil {
		return ErrNilApplier
	}
	if dst == nil {
		return ErrTargetIsNil
	}
	v := reflect.ValueOf(dst).Elem()
	if v.Kind() != reflect.Struct {
		return ErrTargetMustBeStruct
	}

	var undo []func()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrApplyPanic, recovered)
		}
		if err != nil {
			for _, revert := range slices.Backward(undo) {
				revert()
			}
		}
	}()

	state := traversal{
		applier: a,
		undo:    &undo,
		visited: make(map[visitKey]struct{}),
		types:   make(map[reflect.Type]int),
	}
	if err = state.applyStruct(v, "", 0); err != nil {
		return err
	}
	for _, s := range state.postSetters {
		s.SetDefaults()
	}
	if setter, ok := any(dst).(Setter); ok {
		setter.SetDefaults()
	}
	return nil
}

// New allocates a T value, populates its defaults, and returns it.
func (a *Applier) New[T any]() (T, error) {
	var target T
	if err := a.Apply(&target); err != nil {
		var zero T
		return zero, err
	}
	return target, nil
}

// MustNew allocates a T value, populates its defaults, or panics on error.
func (a *Applier) MustNew[T any]() T {
	v, err := a.New[T]()
	if err != nil {
		panic(err)
	}
	return v
}

func resolveApplier(opts ...Option) (*Applier, error) {
	if len(opts) == 0 {
		return defaultApplier, nil
	}
	return NewApplier(opts...)
}

// Apply populates defaults on *dst using opts (reusing cached defaults when empty).
func Apply[T any](dst *T, opts ...Option) error {
	a, err := resolveApplier(opts...)
	if err != nil {
		return err
	}
	return a.Apply(dst)
}

// New creates and initializes a T value with defaults.
func New[T any](opts ...Option) (T, error) {
	a, err := resolveApplier(opts...)
	if err != nil {
		var zero T
		return zero, err
	}
	return a.New[T]()
}

// MustNew creates and initializes a T value with defaults or panics on error.
func MustNew[T any](opts ...Option) T {
	v, err := New[T](opts...)
	if err != nil {
		panic(err)
	}
	return v
}

type visitKey struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

type traversal struct {
	applier     *Applier
	undo        *[]func()
	visited     map[visitKey]struct{}
	types       map[reflect.Type]int
	postSetters []Setter
}

func (t *traversal) pushUndo(fn func()) {
	*t.undo = append(*t.undo, fn)
}

func (t *traversal) applyStruct(value reflect.Value, path string, depth int) error {
	if depth > t.applier.cfg.maxDepth {
		return fmt.Errorf("%w at %s", ErrMaxDepthExceeded, path)
	}
	t.types[value.Type()]++
	defer func() { t.types[value.Type()]-- }()

	plan := planFor(value.Type(), t.applier.cfg.tagName)
	for _, fp := range plan.fields {
		if fp.skip {
			continue
		}
		if t.applier.cfg.filter != nil && !t.applier.cfg.filter(fp.field) {
			continue
		}
		fv := value.Field(fp.index)
		fieldPath := joinPath(path, fp.field.Name)

		if fp.hasDefault && fv.IsZero() {
			t.pushUndo(func() { fv.SetZero() })
			cached := fp.cachedVal
			if t.applier.decoder.hasCustom(fp.field.Type) {
				cached = reflect.Value{}
			}
			composite, err := t.applier.decoder.decode(fv, fp.raw, cached)
			if err != nil {
				return fieldError(fieldPath, fv.Type(), t.applier.cfg.tagName, err)
			}
			if !composite {
				continue
			}
		}

		if err := t.descend(fv, fieldPath, depth+1); err != nil {
			return err
		}
	}
	if depth > 0 && value.CanAddr() {
		if setter, ok := value.Addr().Interface().(Setter); ok {
			t.postSetters = append(t.postSetters, setter)
		}
	}
	return nil
}

func (t *traversal) descend(value reflect.Value, path string, depth int) error {
	if depth > t.applier.cfg.maxDepth {
		return fmt.Errorf("%w at %s", ErrMaxDepthExceeded, path)
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			if t.types[value.Type().Elem()] > 0 {
				return nil
			}
			if !typeHasDefaults(value.Type().Elem(), t.applier.cfg.tagName, t.applier.cfg.filter, make(map[reflect.Type]bool)) {
				return nil
			}
			t.pushUndo(func() { value.SetZero() })
			value.Set(reflect.New(value.Type().Elem()))
		}
		key := visitKey{typ: value.Type(), ptr: value.Pointer()}
		if _, exists := t.visited[key]; exists {
			return nil
		}
		t.visited[key] = struct{}{}
		return t.descend(value.Elem(), path, depth)

	case reflect.Struct:
		if value.Type() == timeLocType || value.Type() == regexpType {
			return nil
		}
		return t.applyStruct(value, path, depth)

	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		key := visitKey{typ: value.Type(), ptr: value.Pointer(), length: value.Len()}
		if _, exists := t.visited[key]; exists {
			return nil
		}
		t.visited[key] = struct{}{}
		for i := range value.Len() {
			if err := t.descend(value.Index(i), path+"["+strconv.Itoa(i)+"]", depth+1); err != nil {
				return err
			}
		}

	case reflect.Array:
		for i := range value.Len() {
			if err := t.descend(value.Index(i), path+"["+strconv.Itoa(i)+"]", depth+1); err != nil {
				return err
			}
		}

	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		key := visitKey{typ: value.Type(), ptr: value.Pointer()}
		if _, exists := t.visited[key]; exists {
			return nil
		}
		t.visited[key] = struct{}{}
		iter := value.MapRange()
		for iter.Next() {
			mapKey := iter.Key()
			prevVal := iter.Value()
			mv := reflect.New(value.Type().Elem()).Elem()
			mv.Set(prevVal)
			mapPath := path + "[" + fmt.Sprint(mapKey.Interface()) + "]"
			t.pushUndo(func() { value.SetMapIndex(mapKey, prevVal) })
			startSetters := len(t.postSetters)
			if err := t.descend(mv, mapPath, depth+1); err != nil {
				return err
			}
			for _, s := range t.postSetters[startSetters:] {
				s.SetDefaults()
			}
			t.postSetters = t.postSetters[:startSetters]
			value.SetMapIndex(mapKey, mv)
		}

	case reflect.Interface:
		if value.IsNil() {
			return nil
		}
		prev := reflect.New(value.Type()).Elem()
		prev.Set(value)
		concrete := reflect.New(value.Elem().Type()).Elem()
		concrete.Set(value.Elem())
		t.pushUndo(func() { value.Set(prev) })
		startSetters := len(t.postSetters)
		if err := t.descend(concrete, path, depth+1); err != nil {
			return err
		}
		for _, s := range t.postSetters[startSetters:] {
			s.SetDefaults()
		}
		t.postSetters = t.postSetters[:startSetters]
		value.Set(concrete)
	}
	return nil
}

func joinPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
}
