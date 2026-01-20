package tag

import (
	"reflect"
	"strings"
)

// ApplyDefaults sets default values for struct fields based on struct tags.
// The target must be a pointer to a struct.
//
// Example:
//
//	type Config struct {
//	    Host string `default:"localhost"`
//	    Port int    `default:"8080"`
//	}
//	config := &Config{}
//	err := ApplyDefaults(config)
func ApplyDefaults(target any, opts ...Option) error {
	options := newOptions(opts)

	valueOf := reflect.ValueOf(target)
	if valueOf.Kind() != reflect.Pointer {
		return ErrTargetMustBePointer
	}
	if valueOf.IsNil() {
		return ErrTargetIsNil
	}

	elem := valueOf.Elem()
	if elem.Kind() != reflect.Struct {
		return ErrUnsupportedType
	}

	ctx := &context{
		options: options,
		depth:   0,
		path:    "",
	}

	return ctx.applyStruct(elem)
}

// context holds the state during processing
type context struct {
	options *Options
	depth   int
	path    string
}

// applyStruct processes a struct and sets defaults for its fields
func (ctx *context) applyStruct(value reflect.Value) error {
	if ctx.depth >= ctx.options.maxDepth {
		return ErrMaxDepthExceeded
	}
	ctx.depth++
	defer func() { ctx.depth-- }()

	typ := value.Type()
	numField := typ.NumField()

	for i := 0; i < numField; i++ {
		field := typ.Field(i)
		fieldValue := value.Field(i)

		// Skip unexported fields
		if !fieldValue.CanSet() {
			continue
		}

		// Apply filter if provided
		if ctx.options.filter != nil && !ctx.options.filter(field) {
			continue
		}

		// Build field path for error reporting
		fieldPath := ctx.buildPath(field.Name)

		tagValue := field.Tag.Get(ctx.options.tagName)

		if err := ctx.applyField(fieldValue, field, tagValue, fieldPath); err != nil {
			return err
		}
	}

	return nil
}

// applyField processes a single field
func (ctx *context) applyField(fieldValue reflect.Value, field reflect.StructField, tagValue, fieldPath string) error {
	// Handle slices with existing elements first (before checking zero)
	if fieldValue.Kind() == reflect.Slice && !fieldValue.IsNil() && fieldValue.Len() > 0 {
		if err := ctx.applySliceElements(fieldValue, fieldPath); err != nil {
			return err
		}
		// After processing existing elements, don't process the slice itself
		return nil
	}

	// Skip if field is not zero (already has a value)
	if !fieldValue.IsZero() {
		return nil
	}

	// No tag value and not a struct - skip
	if tagValue == "" && fieldValue.Kind() != reflect.Struct {
		return nil
	}

	// Apply default value based on field type
	return ctx.applyValue(fieldValue, field.Type, tagValue, fieldPath)
}

// applyValue sets the value based on its kind
func (ctx *context) applyValue(value reflect.Value, typ reflect.Type, tagValue, path string) error {
	kind := value.Kind()

	switch kind {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool:
		if tagValue == "" {
			return nil
		}
		if err := ctx.options.parser.Parse(value, tagValue); err != nil {
			return newFieldError(path, kind, ctx.options.tagName, tagValue, err)
		}

	case reflect.Pointer:
		return ctx.applyPointer(value, typ, tagValue, path)

	case reflect.Slice:
		if tagValue == "" {
			return nil
		}
		return ctx.applySlice(value, tagValue, path)

	case reflect.Struct:
		return ctx.applyStruct(value)

	case reflect.Map:
		return ctx.applyMap(value, tagValue, path)

	default:
		return newFieldError(path, kind, ctx.options.tagName, tagValue, ErrUnsupportedType)
	}

	return nil
}

// applyPointer handles pointer fields
func (ctx *context) applyPointer(value reflect.Value, typ reflect.Type, tagValue, path string) error {
	if tagValue == "" && typ.Elem().Kind() != reflect.Struct {
		return nil
	}

	// Create new instance
	newValue := reflect.New(typ.Elem())
	value.Set(newValue)

	// If it's a pointer to struct, process recursively
	if typ.Elem().Kind() == reflect.Struct {
		return ctx.applyStruct(newValue.Elem())
	}

	// Otherwise, set the value using the tag
	if tagValue != "" {
		return ctx.applyValue(newValue.Elem(), typ.Elem(), tagValue, path)
	}

	return nil
}

// applySlice handles slice fields
func (ctx *context) applySlice(value reflect.Value, tagValue, path string) error {
	if err := ctx.options.parser.Parse(value, tagValue); err != nil {
		return newFieldError(path, value.Kind(), ctx.options.tagName, tagValue, err)
	}
	return nil
}

// applySliceElements processes existing slice elements
func (ctx *context) applySliceElements(value reflect.Value, path string) error {
	for i := 0; i < value.Len(); i++ {
		elem := value.Index(i)

		if elem.Kind() == reflect.Struct {
			if err := ctx.applyStruct(elem); err != nil {
				return err
			}
		} else if elem.Kind() == reflect.Pointer && elem.Elem().Kind() == reflect.Struct {
			if !elem.IsNil() {
				if err := ctx.applyStruct(elem.Elem()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// applyMap handles map fields (basic support)
func (ctx *context) applyMap(value reflect.Value, tagValue, path string) error {
	if tagValue == "" {
		return nil
	}

	mapType := value.Type()
	newMap := reflect.MakeMap(mapType)

	// Simple key:value,key:value format
	pairs := strings.SplitSeq(tagValue, ctx.options.separator)
	for pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}

		keyStr := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])

		key := reflect.New(mapType.Key()).Elem()
		val := reflect.New(mapType.Elem()).Elem()

		if isBasicKind(key.Kind()) {
			if err := ctx.options.parser.Parse(key, keyStr); err != nil {
				return newFieldError(path+"[key]", key.Kind(), ctx.options.tagName, keyStr, err)
			}
		}

		if isBasicKind(val.Kind()) {
			if err := ctx.options.parser.Parse(val, valStr); err != nil {
				return newFieldError(path+"[value]", val.Kind(), ctx.options.tagName, valStr, err)
			}
		}

		newMap.SetMapIndex(key, val)
	}

	value.Set(newMap)
	return nil
}

// buildPath constructs a field path for error messages
func (ctx *context) buildPath(fieldName string) string {
	if ctx.path == "" {
		return fieldName
	}
	return ctx.path + "." + fieldName
}
