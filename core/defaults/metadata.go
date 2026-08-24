package defaults

import (
	"reflect"
	"sync"
)

type metadataKey struct {
	typ     reflect.Type
	tagName string
}

type fieldMetadata struct {
	index      int
	field      reflect.StructField
	hasDefault bool
	value      string
}

type structMetadata struct {
	fields []fieldMetadata
}

var metadataCache sync.Map

func metadataFor(typ reflect.Type, tagName string) *structMetadata {
	key := metadataKey{typ: typ, tagName: tagName}
	if cached, ok := metadataCache.Load(key); ok {
		return cached.(*structMetadata)
	}

	metadata := &structMetadata{fields: make([]fieldMetadata, 0, typ.NumField())}
	for index := range typ.NumField() {
		field := typ.Field(index)
		if !field.IsExported() {
			continue
		}
		value, exists := field.Tag.Lookup(tagName)
		metadata.fields = append(metadata.fields, fieldMetadata{
			index:      index,
			field:      field,
			hasDefault: exists,
			value:      value,
		})
	}

	actual, _ := metadataCache.LoadOrStore(key, metadata)
	return actual.(*structMetadata)
}

func typeHasDefaults(typ reflect.Type, tagName string, visiting map[reflect.Type]bool) bool {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || visiting[typ] {
		return false
	}
	visiting[typ] = true
	defer delete(visiting, typ)

	for _, field := range metadataFor(typ, tagName).fields {
		if field.hasDefault && field.value != "-" {
			return true
		}
		if typeHasDefaults(field.field.Type, tagName, visiting) {
			return true
		}
	}
	return false
}
