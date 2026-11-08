package httpx

import (
	"encoding/json/v2"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
)

// Body describes a request payload.
//
// Encode must be safe to call repeatedly for retries. Callers must not modify the returned bytes.
// Nil data means no body; an empty contentType leaves Content-Type unset.
type Body interface {
	Encode() (data []byte, contentType string, err error)
}

// bodyFunc adapts a function to Body.
type bodyFunc func() ([]byte, string, error)

func (f bodyFunc) Encode() ([]byte, string, error) {
	return f()
}

// NoBody represents an absent request body, equivalent to a nil Body.
func NoBody() Body {
	return bodyFunc(func() ([]byte, string, error) { return nil, "", nil })
}

// JSON encodes v as an application/json request body.
func JSON(v any) Body {
	return bodyFunc(func() ([]byte, string, error) {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, "", fmt.Errorf("httpx: marshal json body: %w", err)
		}
		return data, ContentTypeJSON, nil
	})
}

// XML encodes v as an application/xml request body.
func XML(v any) Body {
	return bodyFunc(func() ([]byte, string, error) {
		data, err := xml.Marshal(v)
		if err != nil {
			return nil, "", fmt.Errorf("httpx: marshal xml body: %w", err)
		}
		return data, ContentTypeXML, nil
	})
}

// Form encodes values as an application/x-www-form-urlencoded request body.
func Form(values url.Values) Body {
	return bodyFunc(func() ([]byte, string, error) {
		return []byte(values.Encode()), ContentTypeForm, nil
	})
}

// FormMap is a convenience wrapper for a map of string values.
func FormMap(values map[string]string) Body {
	v := make(url.Values, len(values))
	for k, val := range values {
		v.Set(k, val)
	}
	return Form(v)
}

// Raw sends data unchanged; an empty contentType leaves Content-Type unset.
func Raw(contentType string, data []byte) Body {
	return bodyFunc(func() ([]byte, string, error) { return data, contentType, nil })
}

// Text sends a string as text/plain.
func Text(s string) Body {
	return Raw(ContentTypeText, []byte(s))
}

// ReadAll creates a body that reads all bytes from r and uses contentType.
//
// Encoding consumes r and closes it if it implements io.Closer. Do not reuse the reader afterward.
func ReadAll(contentType string, r io.Reader) Body {
	return bodyFunc(func() ([]byte, string, error) {
		if r == nil {
			return nil, contentType, nil
		}
		data, err := io.ReadAll(r)
		if c, ok := r.(io.Closer); ok {
			_ = c.Close()
		}
		if err != nil {
			return nil, "", fmt.Errorf("httpx: read body: %w", err)
		}
		return data, contentType, nil
	})
}
