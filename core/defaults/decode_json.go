package defaults

import (
	"encoding/json/v2"
	"errors"
	"reflect"
)

func (d *builtinDecoder) decodeJSON(value reflect.Value, raw string) error {
	return json.Unmarshal([]byte(raw), value.Addr().Interface(), d.jsonOptions)
}

// Register the text parser and its JSON string adapter together.
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
