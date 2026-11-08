package validator

import (
	"fmt"
	"reflect"
	"strings"
)

// validateOptions checks configuration before registration; the underlying API validates reserved tags.
func validateOptions(o *options) error {
	if o.invalidArity {
		return fmt.Errorf("%w: at most one callEvenIfNull value is allowed", ErrInvalidOption)
	}
	if _, ok := o.locales[o.defaultLocale]; !ok {
		return fmt.Errorf("%w: default locale %q not in enabled locales", ErrInvalidOption, o.defaultLocale)
	}
	names := make(map[string]bool, len(o.locales))
	for locale, entry := range o.locales {
		if strings.TrimSpace(string(locale)) == "" || nilInterface(entry.Loc) || entry.Register == nil {
			return fmt.Errorf("%w: incomplete locale %q", ErrInvalidOption, locale)
		}
		name := entry.Loc.Locale()
		if name == "" || names[name] {
			return fmt.Errorf("%w: empty or duplicate translator locale %q", ErrInvalidOption, name)
		}
		names[name] = true
	}
	tags := make(map[string]bool, len(o.validations))
	for _, cv := range o.validations {
		if cv.tag == "" {
			return fmt.Errorf("%w: function Key cannot be empty", ErrInvalidOption)
		}
		if cv.fn == nil && cv.ctxFn == nil {
			return fmt.Errorf("%w: function cannot be empty", ErrInvalidOption)
		}
		if tags[cv.tag] {
			return fmt.Errorf("%w: duplicate validation %q", ErrInvalidOption, cv.tag)
		}
		tags[cv.tag] = true
	}
	for _, sv := range o.structValidations {
		if (sv.fn == nil && sv.ctxFn == nil) || len(sv.types) == 0 {
			return fmt.Errorf("%w: struct validation needs a callback and struct types", ErrInvalidOption)
		}
		for _, value := range sv.types {
			typ := reflect.TypeOf(value)
			for typ != nil && typ.Kind() == reflect.Pointer {
				typ = typ.Elem()
			}
			if typ == nil || typ.Kind() != reflect.Struct {
				return fmt.Errorf("%w: struct validation requires struct types", ErrInvalidOption)
			}
		}
	}
	return nil
}

// nilInterface detects both nil interfaces and typed nil values stored in interfaces.
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}
