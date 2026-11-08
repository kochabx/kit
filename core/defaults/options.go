package defaults

import (
	"fmt"
	"reflect"
	"slices"
)

const defaultMaxDepth = 64

// FieldFilter must be safe for concurrent use when its Applier is shared.
type FieldFilter func(reflect.StructField) bool

// Decoder converts a tag string into a settable value. Implementations must
// be safe for concurrent use when their Applier is shared.
type Decoder interface {
	Decode(reflect.Value, string) error
}

type DecoderFunc func(reflect.Value, string) error

func (f DecoderFunc) Decode(value reflect.Value, raw string) error {
	return f(value, raw)
}

type config struct {
	tagName     string
	maxDepth    int
	filter      FieldFilter
	decoder     Decoder
	timeLayouts []string
}

type Option func(*config) error

func WithTag(name string) Option {
	return func(c *config) error {
		if name == "" {
			return fmt.Errorf("%w: empty tag name", ErrInvalidOption)
		}
		c.tagName = name
		return nil
	}
}

func WithMaxDepth(depth int) Option {
	return func(c *config) error {
		if depth <= 0 {
			return fmt.Errorf("%w: max depth must be positive", ErrInvalidOption)
		}
		c.maxDepth = depth
		return nil
	}
}

func WithFieldFilter(filter FieldFilter) Option {
	return func(c *config) error {
		if filter == nil {
			return fmt.Errorf("%w: nil field filter", ErrInvalidOption)
		}
		c.filter = filter
		return nil
	}
}

func WithDecoder(decoder Decoder) Option {
	return func(c *config) error {
		if isNil(decoder) {
			return fmt.Errorf("%w: nil decoder", ErrInvalidOption)
		}
		c.decoder = decoder
		return nil
	}
}

func WithTimeLayouts(layouts ...string) Option {
	return func(c *config) error {
		if len(layouts) == 0 {
			return fmt.Errorf("%w: no time layouts", ErrInvalidOption)
		}
		if slices.Contains(layouts, "") {
			return fmt.Errorf("%w: empty time layout", ErrInvalidOption)
		}
		c.timeLayouts = append([]string(nil), layouts...)
		return nil
	}
}

func defaultConfig() config {
	return config{
		tagName:     "default",
		maxDepth:    defaultMaxDepth,
		timeLayouts: []string{timeRFC3339Nano, timeRFC3339, timeDateOnly},
	}
}
