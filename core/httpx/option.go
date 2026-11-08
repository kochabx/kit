package httpx

import (
	"encoding/base64"
	"encoding/json/v2"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RequestOption configures a single request.
type RequestOption func(*requestConfig)

// requestConfig holds mutable per-request settings populated by RequestOption.
type requestConfig struct {
	header http.Header
	query  url.Values
	decode func(*http.Response) error
}

// Header appends a header value; repeated calls with the same key retain all values.
func Header(key, value string) RequestOption {
	return func(c *requestConfig) { c.header.Add(key, value) }
}

// SetHeader sets a header, replacing existing values for the key.
func SetHeader(key, value string) RequestOption {
	return func(c *requestConfig) { c.header.Set(key, value) }
}

// Headers sets multiple headers, replacing existing values for matching keys.
func Headers(headers map[string]string) RequestOption {
	return func(c *requestConfig) {
		for k, v := range headers {
			c.header.Set(k, v)
		}
	}
}

// Bearer sets Authorization: Bearer <token>.
func Bearer(token string) RequestOption {
	return SetHeader("Authorization", "Bearer "+token)
}

// BasicAuth sets the HTTP Basic authentication header.
func BasicAuth(username, password string) RequestOption {
	creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return SetHeader("Authorization", "Basic "+creds)
}

// Query appends a query parameter value.
func Query(key, value string) RequestOption {
	return func(c *requestConfig) { c.query.Add(key, value) }
}

// SetQuery sets a query parameter, replacing existing values for the key.
func SetQuery(key, value string) RequestOption {
	return func(c *requestConfig) { c.query.Set(key, value) }
}

// Queries appends multiple query parameter values.
func Queries(values url.Values) RequestOption {
	return func(c *requestConfig) {
		for k, vs := range values {
			for _, v := range vs {
				c.query.Add(k, v)
			}
		}
	}
}

// Into selects a decoder from the response Content-Type:
//   - application/json => json.Unmarshal
//   - application/xml / text/xml => xml.Unmarshal
//   - *[]byte and *string targets receive the raw response body
//
// The response body is closed after decoding. Responses classified as HTTPError are not decoded.
func Into(dest any) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			return decodeAuto(resp, dest)
		}
	}
}

// IntoJSON decodes JSON into dest regardless of the response Content-Type.
func IntoJSON(dest any) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			return json.UnmarshalRead(resp.Body, dest)
		}
	}
}

// IntoXML decodes XML into dest regardless of the response Content-Type.
func IntoXML(dest any) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			return xml.NewDecoder(resp.Body).Decode(dest)
		}
	}
}

// IntoBytes stores the raw response body in dest.
func IntoBytes(dest *[]byte) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			*dest = data
			return nil
		}
	}
}

// IntoString stores the raw response body in dest.
func IntoString(dest *string) RequestOption {
	return func(c *requestConfig) {
		c.decode = func(resp *http.Response) error {
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			*dest = string(data)
			return nil
		}
	}
}

// Decode sets a custom response handler. The handler must close resp.Body.
func Decode(fn func(*http.Response) error) RequestOption {
	return func(c *requestConfig) { c.decode = fn }
}

// decodeAuto selects a decoder from Content-Type.
func decodeAuto(resp *http.Response, dest any) error {
	defer resp.Body.Close()

	switch d := dest.(type) {
	case *[]byte:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*d = data
		return nil
	case *string:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*d = string(data)
		return nil
	}

	ct := resp.Header.Get("Content-Type")
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch {
	case mediaType == "" || strings.Contains(mediaType, "json"):
		return json.UnmarshalRead(resp.Body, dest)
	case strings.Contains(mediaType, "xml"):
		return xml.NewDecoder(resp.Body).Decode(dest)
	default:
		return fmt.Errorf("httpx: cannot auto-decode Content-Type %q into %T", ct, dest)
	}
}
