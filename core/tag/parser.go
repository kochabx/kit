package tag

import (
	"encoding"
	"reflect"
	"strconv"
	"strings"
	"time"
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
		if value.Type() == reflect.TypeFor[time.Duration]() {
			return p.parseDuration(value, str)
		}
		return p.parseInt(value, str)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return p.parseUint(value, str)

	case reflect.Float32, reflect.Float64:
		return p.parseFloat(value, str)

	case reflect.Bool:
		return p.parseBool(value, str)

	case reflect.Slice:
		if value.Type() == reflect.TypeFor[[]byte]() {
			return p.parseBytes(value, str)
		}
		return p.parseSlice(value, str)

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

// parseDuration parses time.Duration values
func (p *defaultParser) parseDuration(value reflect.Value, str string) error {
	str = strings.TrimSpace(str)
	parsed, err := time.ParseDuration(str)
	if err != nil {
		return err
	}
	value.SetInt(int64(parsed))
	return nil
}

// parseBytes parses []byte values from string
func (p *defaultParser) parseBytes(value reflect.Value, str string) error {
	value.SetBytes([]byte(str))
	return nil
}

// parseSlice parses slice values from comma-separated string
func (p *defaultParser) parseSlice(value reflect.Value, str string) error {
	str = strings.TrimSpace(str)
	if str == "" {
		value.Set(reflect.MakeSlice(value.Type(), 0, 0))
		return nil
	}

	parts := strings.Split(str, ",")
	elemType := value.Type().Elem()
	slice := reflect.MakeSlice(value.Type(), len(parts), len(parts))

	for i, part := range parts {
		elem := slice.Index(i)
		if err := p.parseSliceElement(elem, elemType, strings.TrimSpace(part)); err != nil {
			return err
		}
	}

	value.Set(slice)
	return nil
}

// parseSliceElement parses a single slice element
func (p *defaultParser) parseSliceElement(elem reflect.Value, elemType reflect.Type, str string) error {
	switch elemType.Kind() {
	case reflect.String:
		elem.SetString(str)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if elemType == reflect.TypeFor[time.Duration]() {
			parsed, err := time.ParseDuration(str)
			if err != nil {
				return err
			}
			elem.SetInt(int64(parsed))
			return nil
		}
		parsed, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return err
		}
		elem.SetInt(parsed)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(str, 10, 64)
		if err != nil {
			return err
		}
		elem.SetUint(parsed)
		return nil

	case reflect.Float32, reflect.Float64:
		bits := 64
		if elemType.Kind() == reflect.Float32 {
			bits = 32
		}
		parsed, err := strconv.ParseFloat(str, bits)
		if err != nil {
			return err
		}
		elem.SetFloat(parsed)
		return nil

	case reflect.Bool:
		parsed, err := strconv.ParseBool(str)
		if err != nil {
			return err
		}
		elem.SetBool(parsed)
		return nil

	default:
		return ErrUnsupportedType
	}
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
