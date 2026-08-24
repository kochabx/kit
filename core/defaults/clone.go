package defaults

import "reflect"

type cloneVisit struct {
	typ reflect.Type
	ptr unsafePointer
}

type unsafePointer uintptr

func deepClone(source reflect.Value) reflect.Value {
	return cloneValue(source, make(map[cloneVisit]reflect.Value))
}

func cloneValue(source reflect.Value, visited map[cloneVisit]reflect.Value) reflect.Value {
	if !source.IsValid() {
		return source
	}
	switch source.Kind() {
	case reflect.Pointer:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		key := cloneVisit{typ: source.Type(), ptr: unsafePointer(source.Pointer())}
		if cloned, ok := visited[key]; ok {
			return cloned
		}
		cloned := reflect.New(source.Type().Elem())
		visited[key] = cloned
		cloned.Elem().Set(cloneValue(source.Elem(), visited))
		return cloned

	case reflect.Interface:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		cloned := cloneValue(source.Elem(), visited)
		result := reflect.New(source.Type()).Elem()
		result.Set(cloned)
		return result

	case reflect.Struct:
		result := reflect.New(source.Type()).Elem()
		result.Set(source)
		for index := range source.NumField() {
			field := result.Field(index)
			if field.CanSet() && source.Type().Field(index).IsExported() {
				field.Set(cloneValue(source.Field(index), visited))
			}
		}
		return result

	case reflect.Slice:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		key := cloneVisit{typ: source.Type(), ptr: unsafePointer(source.Pointer())}
		if cloned, ok := visited[key]; ok {
			return cloned
		}
		result := reflect.MakeSlice(source.Type(), source.Len(), source.Cap())
		visited[key] = result
		for index := range source.Len() {
			result.Index(index).Set(cloneValue(source.Index(index), visited))
		}
		return result

	case reflect.Map:
		if source.IsNil() {
			return reflect.Zero(source.Type())
		}
		key := cloneVisit{typ: source.Type(), ptr: unsafePointer(source.Pointer())}
		if cloned, ok := visited[key]; ok {
			return cloned
		}
		result := reflect.MakeMapWithSize(source.Type(), source.Len())
		visited[key] = result
		iterator := source.MapRange()
		for iterator.Next() {
			result.SetMapIndex(
				cloneValue(iterator.Key(), visited),
				cloneValue(iterator.Value(), visited),
			)
		}
		return result

	case reflect.Array:
		result := reflect.New(source.Type()).Elem()
		for index := range source.Len() {
			result.Index(index).Set(cloneValue(source.Index(index), visited))
		}
		return result

	default:
		return source
	}
}
