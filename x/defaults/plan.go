package defaults

import (
	"encoding"
	"encoding/json/v2"
	"reflect"
	"strings"
	"sync"
	"time"
)

type planKey struct {
	typ     reflect.Type
	tagName string
}

type fieldPlan struct {
	index      int
	field      reflect.StructField
	hasDefault bool
	skip       bool
	raw        string
	cachedVal  reflect.Value
}

type typePlan struct {
	fields []fieldPlan
}

var planCache sync.Map

func planFor(typ reflect.Type, tagName string) *typePlan {
	key := planKey{typ: typ, tagName: tagName}
	if cached, ok := planCache.Load(key); ok {
		return cached.(*typePlan)
	}

	plan := &typePlan{fields: make([]fieldPlan, 0, typ.NumField())}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		raw, exists := f.Tag.Lookup(tagName)
		fp := fieldPlan{
			index:      i,
			field:      f,
			hasDefault: exists,
			skip:       exists && raw == "-",
			raw:        raw,
		}
		if exists && !fp.skip {
			fp.cachedVal = compileCachedScalar(f.Type, raw)
		}
		plan.fields = append(plan.fields, fp)
	}

	actual, _ := planCache.LoadOrStore(key, plan)
	return actual.(*typePlan)
}

var (
	textUnmarshalerIface = reflect.TypeFor[encoding.TextUnmarshaler]()
	jsonFromIface        = reflect.TypeFor[json.UnmarshalerFrom]()
)

func compileCachedScalar(typ reflect.Type, raw string) reflect.Value {
	if typ.Kind() == reflect.Pointer {
		return reflect.Value{}
	}
	ptrType := reflect.PointerTo(typ)
	if ptrType.Implements(textUnmarshalerIface) || ptrType.Implements(jsonFromIface) {
		return reflect.Value{}
	}
	if typ == durationType {
		if d, err := time.ParseDuration(strings.TrimSpace(raw)); err == nil {
			return reflect.ValueOf(d)
		}
		return reflect.Value{}
	}
	if parsed, ok, err := parsePrimitive(typ, raw); ok && err == nil {
		return parsed
	}
	return reflect.Value{}
}

// typeHasDefaults checks whether allocating a nil pointer to typ can meaningfully
// populate any default inside typ, honoring filter and rejecting pointer-to-nil collections.
func typeHasDefaults(
	typ reflect.Type,
	tagName string,
	filter func(reflect.StructField) bool,
	visiting map[reflect.Type]bool,
) bool {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || visiting[typ] {
		return false
	}
	visiting[typ] = true
	defer delete(visiting, typ)

	for _, f := range planFor(typ, tagName).fields {
		if f.skip {
			continue
		}
		if filter != nil && !filter(f.field) {
			continue
		}
		if f.hasDefault {
			return true
		}
		if typeHasDefaults(f.field.Type, tagName, filter, visiting) {
			return true
		}
	}
	return false
}
