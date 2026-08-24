package defaults

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	timeRFC3339Nano = time.RFC3339Nano
	timeRFC3339     = time.RFC3339
	timeDateOnly    = time.DateOnly
)

var (
	durationType = reflect.TypeFor[time.Duration]()
	timeType     = reflect.TypeFor[time.Time]()
	locationType = reflect.TypeFor[time.Location]()
	urlType      = reflect.TypeFor[url.URL]()
	ipType       = reflect.TypeFor[net.IP]()
	ipNetType    = reflect.TypeFor[net.IPNet]()
	regexpType   = reflect.TypeFor[regexp.Regexp]()
)

type builtinDecoder struct {
	timeLayouts []string
}

func (d builtinDecoder) Decode(value reflect.Value, raw string) error {
	if value.Kind() == reflect.Pointer {
		decoded := reflect.New(value.Type().Elem())
		if err := d.Decode(decoded.Elem(), raw); err != nil {
			return err
		}
		value.Set(decoded)
		return nil
	}

	if handled, err := d.decodeKnownType(value, raw); handled {
		return err
	}
	if value.CanAddr() {
		if unmarshaler, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(raw))
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
		return decodeJSON(value, raw)
	default:
		return ErrUnsupportedType
	}
	return nil
}

func (d builtinDecoder) decodeKnownType(value reflect.Value, raw string) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	switch value.Type() {
	case durationType:
		parsed, err := time.ParseDuration(trimmed)
		if err == nil {
			value.SetInt(int64(parsed))
		}
		return true, err
	case timeType:
		for _, layout := range d.timeLayouts {
			parsed, err := time.Parse(layout, trimmed)
			if err == nil {
				value.Set(reflect.ValueOf(parsed))
				return true, nil
			}
		}
		return true, errors.New("time does not match configured layouts")
	case locationType:
		location, err := time.LoadLocation(trimmed)
		if err == nil {
			value.Set(reflect.ValueOf(*location))
		}
		return true, err
	case urlType:
		parsed, err := url.Parse(trimmed)
		if err == nil {
			value.Set(reflect.ValueOf(*parsed))
		}
		return true, err
	case ipType:
		parsed := net.ParseIP(trimmed)
		if parsed == nil {
			return true, errors.New("invalid IP address")
		}
		value.Set(reflect.ValueOf(parsed))
		return true, nil
	case ipNetType:
		ip, network, err := net.ParseCIDR(trimmed)
		if err == nil {
			network.IP = ip
			value.Set(reflect.ValueOf(*network))
		}
		return true, err
	case regexpType:
		compiled, err := regexp.Compile(raw)
		if err == nil {
			value.Set(reflect.ValueOf(*compiled))
		}
		return true, err
	}

	if value.Type() == reflect.TypeFor[[]byte]() {
		decoded, err := decodeBytes(raw)
		if err == nil {
			value.SetBytes(decoded)
		}
		return true, err
	}
	return false, nil
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

func decodeJSON(value reflect.Value, raw string) error {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Addr().Interface()); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
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
