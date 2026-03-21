package defaults

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// Apply sets default values for struct fields based on struct tags.
// The target must be a pointer to a struct.
//
// Example:
//
//	type Config struct {
//	    Host string `default:"localhost"`
//	    Port int    `default:"8080"`
//	}
//	config := &Config{}
//	err := defaults.Apply(config)
func Apply(target any, opts ...Option) error {
	options := newOptions(opts)

	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer {
		return ErrTargetMustBePointer
	}
	if v.IsNil() {
		return ErrTargetIsNil
	}

	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return ErrUnsupportedType
	}

	ctx := &context{options: options}
	return ctx.applyStruct(elem)
}

// context holds mutable processing state.
type context struct {
	options *Options
	depth   int
	path    string
}

// --- struct metadata cache ---

type cacheKey struct {
	typ     reflect.Type
	tagName string
}

type fieldMeta struct {
	index    int
	field    reflect.StructField
	tagValue string
}

type structInfo struct {
	fields []fieldMeta
}

var cache sync.Map // map[cacheKey]*structInfo

func getStructInfo(typ reflect.Type, tagName string) *structInfo {
	key := cacheKey{typ: typ, tagName: tagName}
	if v, ok := cache.Load(key); ok {
		return v.(*structInfo)
	}

	n := typ.NumField()
	fields := make([]fieldMeta, 0, n)
	for i := range n {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		fields = append(fields, fieldMeta{
			index:    i,
			field:    f,
			tagValue: f.Tag.Get(tagName),
		})
	}

	info := &structInfo{fields: fields}
	v, _ := cache.LoadOrStore(key, info)
	return v.(*structInfo)
}

// --- apply logic ---

// enterStruct increments depth, calls applyStruct, then restores depth.
func (ctx *context) enterStruct(value reflect.Value) error {
	ctx.depth++
	err := ctx.applyStruct(value)
	ctx.depth--
	return err
}

func (ctx *context) applyStruct(value reflect.Value) error {
	if ctx.depth >= ctx.options.maxDepth {
		return ErrMaxDepthExceeded
	}

	info := getStructInfo(value.Type(), ctx.options.tagName)
	savedPath := ctx.path

	for i := range info.fields {
		meta := &info.fields[i]
		fv := value.Field(meta.index)

		if !fv.CanSet() {
			continue
		}

		if ctx.options.filter != nil && !ctx.options.filter(meta.field) {
			continue
		}

		ctx.path = buildPath(savedPath, meta.field.Name)
		if err := ctx.applyField(fv, meta); err != nil {
			return err
		}
	}

	ctx.path = savedPath
	return nil
}

func (ctx *context) applyField(fv reflect.Value, meta *fieldMeta) error {
	kind := fv.Kind()

	// Struct: always recurse (sub-fields may need defaults even if struct is non-zero)
	if kind == reflect.Struct {
		return ctx.enterStruct(fv)
	}

	// Slice with existing elements: recurse into struct elements
	if kind == reflect.Slice && !fv.IsNil() && fv.Len() > 0 {
		return ctx.applySliceElements(fv)
	}

	// Pointer to struct: create if nil, then recurse
	if kind == reflect.Pointer && meta.field.Type.Elem().Kind() == reflect.Struct {
		if fv.IsNil() {
			v := reflect.New(meta.field.Type.Elem())
			fv.Set(v)
			return ctx.enterStruct(v.Elem())
		}
		return ctx.enterStruct(fv.Elem())
	}

	// Non-zero field: already set, skip
	if !fv.IsZero() {
		return nil
	}

	// No tag value: nothing to apply
	if meta.tagValue == "" {
		return nil
	}

	return ctx.applyValue(fv, meta.field.Type, meta.tagValue)
}

func (ctx *context) applyValue(value reflect.Value, typ reflect.Type, tagValue string) error {
	switch value.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool:
		if err := ctx.options.parser.Parse(value, tagValue); err != nil {
			return newFieldError(ctx.path, value.Kind(), ctx.options.tagName, tagValue, err)
		}

	case reflect.Pointer:
		return ctx.applyPointer(value, typ, tagValue)

	case reflect.Slice:
		return ctx.applySlice(value, tagValue)

	case reflect.Map:
		return ctx.applyMap(value, tagValue)

	default:
		return newFieldError(ctx.path, value.Kind(), ctx.options.tagName, tagValue, ErrUnsupportedType)
	}

	return nil
}

func (ctx *context) applyPointer(value reflect.Value, typ reflect.Type, tagValue string) error {
	elemKind := typ.Elem().Kind()

	if tagValue == "" && elemKind != reflect.Struct {
		return nil
	}

	v := reflect.New(typ.Elem())
	value.Set(v)

	if elemKind == reflect.Struct {
		return ctx.enterStruct(v.Elem())
	}

	if tagValue != "" {
		return ctx.applyValue(v.Elem(), typ.Elem(), tagValue)
	}

	return nil
}

var byteSliceType = reflect.TypeFor[[]byte]()

func (ctx *context) applySlice(value reflect.Value, tagValue string) error {
	// []byte: set directly without splitting
	if value.Type() == byteSliceType {
		value.SetBytes([]byte(tagValue))
		return nil
	}

	str := strings.TrimSpace(tagValue)
	if str == "" {
		value.Set(reflect.MakeSlice(value.Type(), 0, 0))
		return nil
	}

	parts := strings.Split(str, ctx.options.separator)
	slice := reflect.MakeSlice(value.Type(), len(parts), len(parts))
	elemKind := value.Type().Elem().Kind()

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if err := ctx.options.parser.Parse(slice.Index(i), part); err != nil {
			return newFieldError(ctx.path+"["+strconv.Itoa(i)+"]", elemKind, ctx.options.tagName, part, err)
		}
	}

	value.Set(slice)
	return nil
}

func (ctx *context) applySliceElements(value reflect.Value) error {
	savedPath := ctx.path
	for i := range value.Len() {
		elem := value.Index(i)
		ctx.path = savedPath + "[" + strconv.Itoa(i) + "]"

		switch {
		case elem.Kind() == reflect.Struct:
			if err := ctx.enterStruct(elem); err != nil {
				return err
			}
		case elem.Kind() == reflect.Pointer && !elem.IsNil() && elem.Elem().Kind() == reflect.Struct:
			if err := ctx.enterStruct(elem.Elem()); err != nil {
				return err
			}
		}
	}
	ctx.path = savedPath
	return nil
}

func (ctx *context) applyMap(value reflect.Value, tagValue string) error {
	if tagValue == "" {
		return nil
	}

	mapType := value.Type()
	newMap := reflect.MakeMap(mapType)

	for pair := range strings.SplitSeq(tagValue, ctx.options.separator) {
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
				return newFieldError(ctx.path+"[key]", key.Kind(), ctx.options.tagName, keyStr, err)
			}
		}

		if isBasicKind(val.Kind()) {
			if err := ctx.options.parser.Parse(val, valStr); err != nil {
				return newFieldError(ctx.path+"[value]", val.Kind(), ctx.options.tagName, valStr, err)
			}
		}

		newMap.SetMapIndex(key, val)
	}

	value.Set(newMap)
	return nil
}

func buildPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
}
