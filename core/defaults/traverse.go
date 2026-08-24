package defaults

import (
	"fmt"
	"reflect"
	"strconv"
)

type visit struct {
	typ reflect.Type
	ptr uintptr
}

type traversal struct {
	applier *Applier
	visited map[visit]struct{}
	types   map[reflect.Type]int
}

func (t *traversal) applyStruct(value reflect.Value, path string, depth int) error {
	if depth > t.applier.config.maxDepth {
		return fmt.Errorf("%w at %s", ErrMaxDepthExceeded, path)
	}
	t.types[value.Type()]++
	defer func() { t.types[value.Type()]-- }()

	metadata := metadataFor(value.Type(), t.applier.config.tagName)
	for _, field := range metadata.fields {
		if t.applier.config.filter != nil && !t.applier.config.filter(field.field) {
			continue
		}
		if field.hasDefault && field.value == "-" {
			continue
		}
		fieldValue := value.Field(field.index)
		fieldPath := joinPath(path, field.field.Name)
		if field.hasDefault && fieldValue.IsZero() {
			if err := t.applier.config.decoder.Decode(fieldValue, field.value); err != nil {
				return fieldError(fieldPath, fieldValue.Type(), t.applier.config.tagName, err)
			}
			continue
		}
		if err := t.descend(fieldValue, fieldPath, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (t *traversal) descend(value reflect.Value, path string, depth int) error {
	if depth > t.applier.config.maxDepth {
		return fmt.Errorf("%w at %s", ErrMaxDepthExceeded, path)
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			if t.types[value.Type().Elem()] > 0 {
				return nil
			}
			if !typeHasDefaults(value.Type().Elem(), t.applier.config.tagName, make(map[reflect.Type]bool)) {
				return nil
			}
			value.Set(reflect.New(value.Type().Elem()))
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if _, exists := t.visited[key]; exists {
			return nil
		}
		t.visited[key] = struct{}{}
		return t.descend(value.Elem(), path, depth)

	case reflect.Struct:
		return t.applyStruct(value, path, depth)

	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if _, exists := t.visited[key]; exists {
			return nil
		}
		t.visited[key] = struct{}{}
		for index := range value.Len() {
			if err := t.descend(value.Index(index), indexedPath(path, index), depth+1); err != nil {
				return err
			}
		}

	case reflect.Array:
		for index := range value.Len() {
			if err := t.descend(value.Index(index), indexedPath(path, index), depth+1); err != nil {
				return err
			}
		}

	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if _, exists := t.visited[key]; exists {
			return nil
		}
		t.visited[key] = struct{}{}
		iterator := value.MapRange()
		for iterator.Next() {
			mapValue := reflect.New(value.Type().Elem()).Elem()
			mapValue.Set(iterator.Value())
			mapPath := path + "[" + fmt.Sprint(iterator.Key().Interface()) + "]"
			if err := t.descend(mapValue, mapPath, depth+1); err != nil {
				return err
			}
			value.SetMapIndex(iterator.Key(), mapValue)
		}

	case reflect.Interface:
		if value.IsNil() {
			return nil
		}
		concrete := reflect.New(value.Elem().Type()).Elem()
		concrete.Set(value.Elem())
		if err := t.descend(concrete, path, depth+1); err != nil {
			return err
		}
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

func indexedPath(base string, index int) string {
	return base + "[" + strconv.Itoa(index) + "]"
}
