package tag

import "reflect"

// Options holds configuration for default tag processing
type Options struct {
	tagName   string
	maxDepth  int
	separator string
	parser    ValueParser
	filter    FieldFilter
}

// Option is a function that configures Options
type Option func(*Options)

// WithTagName sets the tag name to look for (default: "default")
func WithTagName(name string) Option {
	return func(o *Options) {
		o.tagName = name
	}
}

// WithMaxDepth sets the maximum recursion depth (default: 32)
func WithMaxDepth(depth int) Option {
	return func(o *Options) {
		o.maxDepth = depth
	}
}

// WithSeparator sets the separator for slice values (default: ",")
func WithSeparator(sep string) Option {
	return func(o *Options) {
		o.separator = sep
	}
}

// WithParser sets a custom value parser
func WithParser(parser ValueParser) Option {
	return func(o *Options) {
		o.parser = parser
	}
}

// WithFieldFilter sets a field filter function
func WithFieldFilter(filter FieldFilter) Option {
	return func(o *Options) {
		o.filter = filter
	}
}

// FieldFilter is a function that determines if a field should be processed
type FieldFilter func(field reflect.StructField) bool

// newOptions creates a new Options with defaults
func newOptions(opts []Option) *Options {
	options := &Options{
		tagName:   "default",
		maxDepth:  32,
		separator: ",",
		parser:    &defaultParser{},
		filter:    nil,
	}

	for _, opt := range opts {
		opt(options)
	}

	return options
}
