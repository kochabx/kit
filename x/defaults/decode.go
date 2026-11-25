package defaults

import (
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type customDecoderFunc func(string) (any, error)

type builtinDecoder struct {
	parsers     map[reflect.Type]func(reflect.Value, string) error
	custom      map[reflect.Type]customDecoderFunc
	jsonOptions json.Options
}

var (
	timeLocType = reflect.TypeFor[time.Location]()
	regexpType  = reflect.TypeFor[regexp.Regexp]()
	bytesType   = reflect.TypeFor[[]byte]()
)

func newBuiltinDecoder(layouts []string, custom map[reflect.Type]customDecoderFunc) *builtinDecoder {
	d := &builtinDecoder{
		parsers: make(map[reflect.Type]func(reflect.Value, string) error, 12),
		custom:  custom,
	}
	parsers := json.JoinUnmarshalers(
		registerText(d, func(raw string) (time.Duration, error) {
			return time.ParseDuration(strings.TrimSpace(raw))
		}),
		registerText(d, func(raw string) (time.Time, error) {
			trimmed := strings.TrimSpace(raw)
			for _, layout := range layouts {
				if parsed, err := time.Parse(layout, trimmed); err == nil {
					return parsed, nil
				}
			}
			return time.Time{}, errors.New("time does not match configured layouts")
		}),
		registerText(d, func(raw string) (*time.Location, error) {
			return time.LoadLocation(strings.TrimSpace(raw))
		}),
		registerText(d, func(raw string) (url.URL, error) {
			parsed, err := url.Parse(strings.TrimSpace(raw))
			if err != nil {
				return url.URL{}, err
			}
			return *parsed, nil
		}),
		registerText(d, url.ParseQuery),
		registerText(d, func(raw string) (net.IP, error) {
			ip := net.ParseIP(strings.TrimSpace(raw))
			if ip == nil {
				return nil, errors.New("invalid IP address")
			}
			return ip, nil
		}),
		registerText(d, func(raw string) (net.IPNet, error) {
			ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
			if err != nil {
				return net.IPNet{}, err
			}
			ipNet.IP = ip
			return *ipNet, nil
		}),
		registerText(d, func(raw string) (net.HardwareAddr, error) {
			return net.ParseMAC(strings.TrimSpace(raw))
		}),
		registerText(d, func(raw string) (net.TCPAddr, error) {
			ap, err := netip.ParseAddrPort(strings.TrimSpace(raw))
			if err != nil {
				return net.TCPAddr{}, err
			}
			return *net.TCPAddrFromAddrPort(ap), nil
		}),
		registerText(d, func(raw string) (net.UDPAddr, error) {
			ap, err := netip.ParseAddrPort(strings.TrimSpace(raw))
			if err != nil {
				return net.UDPAddr{}, err
			}
			return *net.UDPAddrFromAddrPort(ap), nil
		}),
		registerText(d, regexp.Compile),
	)
	d.jsonOptions = json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(parsers))
	return d
}

// registerText registers a parser for struct tags and JSON-embedded strings.
func registerText[T any](d *builtinDecoder, parse func(string) (T, error)) *json.Unmarshalers {
	d.parsers[reflect.TypeFor[T]()] = func(value reflect.Value, raw string) error {
		parsed, err := parse(raw)
		if err == nil {
			value.Set(reflect.ValueOf(parsed))
		}
		return err
	}
	return json.UnmarshalFunc(func(data []byte, target *T) error {
		if len(data) == 0 || data[0] != '"' {
			return errors.ErrUnsupported
		}
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		parsed, err := parse(raw)
		if err == nil {
			*target = parsed
		}
		return err
	})
}

func (d *builtinDecoder) decode(value reflect.Value, raw string, cached reflect.Value) (composite bool, err error) {
	typ := value.Type()
	if fn, ok := d.custom[typ]; ok {
		parsed, err := fn(raw)
		if err != nil {
			return false, err
		}
		pv := reflect.ValueOf(parsed)
		if !pv.IsValid() || !pv.Type().AssignableTo(typ) {
			return false, errors.New("custom decoder returned incompatible type")
		}
		value.Set(pv)
		return false, nil
	}
	if cached.IsValid() && len(d.custom) == 0 && value.Kind() != reflect.Pointer {
		value.Set(cached)
		return false, nil
	}
	if parse, ok := d.parsers[typ]; ok {
		return false, parse(value, raw)
	}
	if typ == timeLocType || typ == regexpType {
		return false, errors.Join(ErrUnsupportedType, errors.New("use pointer type *time.Location or *regexp.Regexp"))
	}

	if value.Kind() == reflect.Pointer {
		decoded := reflect.New(typ.Elem())
		comp, err := d.decode(decoded.Elem(), raw, cached)
		if err != nil {
			return false, err
		}
		value.Set(decoded)
		return comp, nil
	}

	if typ == bytesType {
		parsed, err := decodeBytes(raw)
		if err == nil {
			value.SetBytes(parsed)
		}
		return false, err
	}
	if value.CanAddr() {
		addr := value.Addr().Interface()
		if tu, ok := addr.(encoding.TextUnmarshaler); ok {
			return false, tu.UnmarshalText([]byte(raw))
		}
		if _, ok := addr.(json.UnmarshalerFrom); ok {
			return false, json.Unmarshal([]byte(raw), addr, d.jsonOptions)
		}
	}

	if parsed, ok, err := parsePrimitive(typ, raw); ok {
		if err != nil {
			return false, err
		}
		value.Set(parsed)
		return false, nil
	}

	switch value.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array, reflect.Struct:
		if err := json.Unmarshal([]byte(raw), value.Addr().Interface(), d.jsonOptions); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, ErrUnsupportedType
	}
}

func (d *builtinDecoder) hasCustom(typ reflect.Type) bool {
	if _, ok := d.custom[typ]; ok {
		return true
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
		if _, ok := d.custom[typ]; ok {
			return true
		}
	}
	return false
}

func parsePrimitive(typ reflect.Type, raw string) (reflect.Value, bool, error) {
	trimmed := strings.TrimSpace(raw)
	v := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.String:
		v.SetString(raw)
		return v, true, nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(trimmed)
		if err != nil {
			return reflect.Value{}, true, err
		}
		v.SetBool(parsed)
		return v, true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if typ == durationType {
			return reflect.Value{}, false, nil
		}
		parsed, err := strconv.ParseInt(trimmed, 0, typ.Bits())
		if err != nil {
			return reflect.Value{}, true, err
		}
		v.SetInt(parsed)
		return v, true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(trimmed, 0, typ.Bits())
		if err != nil {
			return reflect.Value{}, true, err
		}
		v.SetUint(parsed)
		return v, true, nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(trimmed, typ.Bits())
		if err != nil {
			return reflect.Value{}, true, err
		}
		v.SetFloat(parsed)
		return v, true, nil
	case reflect.Complex64, reflect.Complex128:
		parsed, err := strconv.ParseComplex(trimmed, typ.Bits())
		if err != nil {
			return reflect.Value{}, true, err
		}
		v.SetComplex(parsed)
		return v, true, nil
	default:
		return reflect.Value{}, false, nil
	}
}

var durationType = reflect.TypeFor[time.Duration]()

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
