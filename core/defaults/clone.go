package defaults

import (
	"fmt"
	"reflect"
)

type cloneVisit struct {
	typ      reflect.Type
	ptr      uintptr
	len, cap int
}

type cloneState struct {
	visited  map[cloneVisit]reflect.Value
	maxDepth int
	err      error
}

func deepClone(source reflect.Value, maxDepth int) (reflect.Value, error) {
	state := cloneState{visited: make(map[cloneVisit]reflect.Value), maxDepth: maxDepth}
	result := state.clone(source, 0)
	return result, state.err
}

func (c *cloneState) clone(source reflect.Value, depth int) reflect.Value {
	if c.err != nil {
		return source
	}
	if depth > c.maxDepth {
		c.err = fmt.Errorf("%w while cloning %s", ErrMaxDepthExceeded, source.Type())
		return source
	}

	if !source.IsValid() {
		return source
	}
	switch source.Kind() {
	case reflect.Pointer:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		key := cloneVisit{typ: source.Type(), ptr: source.Pointer()}
		if cloned, ok := c.visited[key]; ok {
			return cloned
		}
		cloned := reflect.New(source.Type().Elem())
		c.visited[key] = cloned
		cloned.Elem().Set(c.clone(source.Elem(), depth+1))
		return cloned

	case reflect.Interface:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		cloned := c.clone(source.Elem(), depth+1)
		result := reflect.New(source.Type()).Elem()
		result.Set(cloned)
		return result

	case reflect.Struct:
		result := reflect.New(source.Type()).Elem()
		result.Set(source)
		for index := range source.NumField() {
			field := result.Field(index)
			if field.CanSet() && source.Type().Field(index).IsExported() {
				field.Set(c.clone(source.Field(index), depth+1))
			}
		}
		return result

	case reflect.Slice:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		key := cloneVisit{typ: source.Type(), ptr: source.Pointer(), len: source.Len(), cap: source.Cap()}
		if cloned, ok := c.visited[key]; ok {
			return cloned
		}
		result := reflect.MakeSlice(source.Type(), source.Len(), source.Cap())
		c.visited[key] = result
		for index := range source.Len() {
			result.Index(index).Set(c.clone(source.Index(index), depth+1))
		}
		return result

	case reflect.Map:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		key := cloneVisit{typ: source.Type(), ptr: source.Pointer()}
		if cloned, ok := c.visited[key]; ok {
			return cloned
		}
		result := reflect.MakeMapWithSize(source.Type(), source.Len())
		c.visited[key] = result
		iterator := source.MapRange()
		for iterator.Next() {
			result.SetMapIndex(
				c.clone(iterator.Key(), depth+1),
				c.clone(iterator.Value(), depth+1),
			)
		}
		return result

	case reflect.Array:
		result := reflect.New(source.Type()).Elem()
		for index := range source.Len() {
			result.Index(index).Set(c.clone(source.Index(index), depth+1))
		}
		return result

	default:
		return source
	}
}
