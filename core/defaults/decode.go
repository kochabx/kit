package defaults

import (
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	timeRFC3339Nano = time.RFC3339Nano
	timeRFC3339     = time.RFC3339
	timeDateOnly    = time.DateOnly
)

type builtinDecoder struct {
	parsers     map[reflect.Type]func(reflect.Value, string) error
	jsonOptions json.Options
}

func (d *builtinDecoder) Decode(value reflect.Value, raw string) error {
	if value.Kind() == reflect.Pointer {
		decoded := reflect.New(value.Type().Elem())
		if err := d.Decode(decoded.Elem(), raw); err != nil {
			return err
		}
		value.Set(decoded)
		return nil
	}

	if parse, ok := d.parsers[value.Type()]; ok {
		return parse(value, raw)
	}
	if value.Type() == reflect.TypeFor[[]byte]() {
		parsed, err := decodeBytes(raw)
		if err == nil {
			value.SetBytes(parsed)
		}
		return err
	}
	if value.CanAddr() {
		if unmarshaler, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(raw))
		}
		if _, ok := value.Addr().Interface().(json.UnmarshalerFrom); ok {
			return d.decodeJSON(value, raw)
		}
		if unmarshaler, ok := value.Addr().Interface().(json.Unmarshaler); ok {
			return unmarshaler.UnmarshalJSON([]byte(raw))
		}
	}

	trimmed := strings.TrimSpace(raw)
	switch value.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(trimmed)
		if err != nil {
			return err
		}
		value.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(trimmed, 0, value.Type().Bits())
		if err != nil {
			return err
		}
		value.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(trimmed, 0, value.Type().Bits())
		if err != nil {
			return err
		}
		value.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(trimmed, value.Type().Bits())
		if err != nil {
			return err
		}
		value.SetFloat(parsed)
	case reflect.Complex64, reflect.Complex128:
		parsed, err := strconv.ParseComplex(trimmed, value.Type().Bits())
		if err != nil {
			return err
		}
		value.SetComplex(parsed)
	case reflect.Slice, reflect.Map, reflect.Array, reflect.Struct:
		return d.decodeJSON(value, raw)
	default:
		return ErrUnsupportedType
	}
	return nil
}

func decodeBytes(raw string) ([]byte, error) {
	switch {
	case strings.HasPrefix(raw, "base64:"):
		return base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, "base64:"))
	case strings.HasPrefix(raw, "hex:"):
		return hex.DecodeString(strings.TrimPrefix(raw, "hex:"))
	default:
		return []byte(raw), nil
	}
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
