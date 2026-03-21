package defaults

import (
	"encoding"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ValueParser defines the interface for parsing string values into reflect.Value.
type ValueParser interface {
	Parse(value reflect.Value, str string) error
}

var durationType = reflect.TypeFor[time.Duration]()

// defaultParser implements the default parsing logic for scalar types.
type defaultParser struct{}

// Parse parses a string value and sets it to the reflect.Value.
// Supports: string, int*, uint*, float*, bool, time.Duration,
// and any type implementing encoding.TextUnmarshaler.
func (p *defaultParser) Parse(value reflect.Value, str string) error {
	// encoding.TextUnmarshaler takes priority (receives raw string, no trim)
	if value.CanAddr() {
		if u, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText([]byte(str))
		}
	}

	str = strings.TrimSpace(str)

	switch value.Kind() {
	case reflect.String:
		value.SetString(str)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.Type() == durationType {
			d, err := time.ParseDuration(str)
			if err != nil {
				return err
			}
			value.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return err
		}
		value.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(str, 10, 64)
		if err != nil {
			return err
		}
		value.SetUint(n)

	case reflect.Float32, reflect.Float64:
		bits := 64
		if value.Kind() == reflect.Float32 {
			bits = 32
		}
		n, err := strconv.ParseFloat(str, bits)
		if err != nil {
			return err
		}
		value.SetFloat(n)

	case reflect.Bool:
		b, err := strconv.ParseBool(str)
		if err != nil {
			return err
		}
		value.SetBool(b)

	default:
		return ErrUnsupportedType
	}

	return nil
}

// isBasicKind reports whether kind is a scalar type that can be parsed.
func isBasicKind(kind reflect.Kind) bool {
	const basicKinds = (1 << reflect.Bool) |
		(1 << reflect.String) |
		(1 << reflect.Int) | (1 << reflect.Int8) | (1 << reflect.Int16) | (1 << reflect.Int32) | (1 << reflect.Int64) |
		(1 << reflect.Uint) | (1 << reflect.Uint8) | (1 << reflect.Uint16) | (1 << reflect.Uint32) | (1 << reflect.Uint64) |
		(1 << reflect.Float32) | (1 << reflect.Float64)

	return (basicKinds & (1 << kind)) != 0
}
