package tag

import (
	"encoding"
	"reflect"
	"strconv"
	"strings"
)

// ValueParser defines the interface for parsing string values
type ValueParser interface {
	Parse(value reflect.Value, str string) error
}

// defaultParser implements the default parsing logic
type defaultParser struct{}

// Parse parses a string value and sets it to the reflect.Value
func (p *defaultParser) Parse(value reflect.Value, str string) error {
	// Check if type implements encoding.TextUnmarshaler
	if value.CanAddr() {
		if unmarshaler, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(str))
		}
	}

	switch value.Kind() {
	case reflect.String:
		value.SetString(str)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return p.parseInt(value, str)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return p.parseUint(value, str)

	case reflect.Float32, reflect.Float64:
		return p.parseFloat(value, str)

	case reflect.Bool:
		return p.parseBool(value, str)

	default:
		return ErrUnsupportedType
	}
}

// parseInt parses integer values
func (p *defaultParser) parseInt(value reflect.Value, str string) error {
	str = strings.TrimSpace(str)
	parsed, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return err
	}
	value.SetInt(parsed)
	return nil
}

// parseUint parses unsigned integer values
func (p *defaultParser) parseUint(value reflect.Value, str string) error {
	str = strings.TrimSpace(str)
	parsed, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return err
	}
	value.SetUint(parsed)
	return nil
}

// parseFloat parses floating point values
func (p *defaultParser) parseFloat(value reflect.Value, str string) error {
	str = strings.TrimSpace(str)
	bits := 64
	if value.Kind() == reflect.Float32 {
		bits = 32
	}
	parsed, err := strconv.ParseFloat(str, bits)
	if err != nil {
		return err
	}
	value.SetFloat(parsed)
	return nil
}

// parseBool parses boolean values
func (p *defaultParser) parseBool(value reflect.Value, str string) error {
	str = strings.TrimSpace(str)
	parsed, err := strconv.ParseBool(str)
	if err != nil {
		return err
	}
	value.SetBool(parsed)
	return nil
}

// isBasicKind checks if a kind is a basic type that can be parsed
func isBasicKind(kind reflect.Kind) bool {
	// Using bit mask for performance
	const basicKinds = (1 << reflect.Bool) |
		(1 << reflect.String) |
		(1 << reflect.Int) | (1 << reflect.Int8) | (1 << reflect.Int16) | (1 << reflect.Int32) | (1 << reflect.Int64) |
		(1 << reflect.Uint) | (1 << reflect.Uint8) | (1 << reflect.Uint16) | (1 << reflect.Uint32) | (1 << reflect.Uint64) |
		(1 << reflect.Float32) | (1 << reflect.Float64)

	return (basicKinds & (1 << kind)) != 0
}
