package httpx

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

// URLBuilder provides a fluent API for constructing URLs.
type URLBuilder struct {
	scheme   string
	host     string
	port     string
	path     strings.Builder
	query    url.Values
	fragment string
}

// NewURLBuilder creates a URL builder.
func NewURLBuilder() *URLBuilder {
	return &URLBuilder{
		query: make(url.Values),
	}
}

// Scheme sets the URL scheme, such as http, https, or ftp.
func (b *URLBuilder) Scheme(scheme string) *URLBuilder {
	b.scheme = scheme
	return b
}

// Host sets the host address.
func (b *URLBuilder) Host(host string) *URLBuilder {
	b.host = host
	return b
}

// Port sets the port number.
func (b *URLBuilder) Port(port string) *URLBuilder {
	if port != "" && port != "0" {
		b.port = port
	}
	return b
}

// Path replaces the complete path. An empty string clears it.
func (b *URLBuilder) Path(path string) *URLBuilder {
	b.path.Reset()
	if path != "" {
		b.path.WriteString(path)
	}
	return b
}

// AppendPath appends path segments and normalizes slash boundaries.
func (b *URLBuilder) AppendPath(segments ...string) *URLBuilder {
	if len(segments) == 0 {
		return b
	}

	currentPath := b.path.String()

	// Build the new path.
	var pathSegments []string
	if currentPath != "" {
		pathSegments = append(pathSegments, currentPath)
	}

	// Filter out empty segments.
	for _, segment := range segments {
		if segment != "" {
			pathSegments = append(pathSegments, segment)
		}
	}

	if len(pathSegments) > 0 {
		b.path.Reset()
		b.path.WriteString(path.Join(pathSegments...))
	}

	return b
}

// Query appends a query parameter.
func (b *URLBuilder) Query(key, value string) *URLBuilder {
	b.query.Add(key, value)
	return b
}

// SetQuery replaces existing values for a query parameter.
func (b *URLBuilder) SetQuery(key, value string) *URLBuilder {
	b.query.Set(key, value)
	return b
}

// QueryMap appends query parameters from a map.
func (b *URLBuilder) QueryMap(params map[string]string) *URLBuilder {
	for k, v := range params {
		b.query.Add(k, v)
	}
	return b
}

// SetQueryMap sets query parameters, replacing matching names.
func (b *URLBuilder) SetQueryMap(params map[string]string) *URLBuilder {
	for k, v := range params {
		b.query.Set(k, v)
	}
	return b
}

// QuerySlice appends multiple values for a query parameter.
func (b *URLBuilder) QuerySlice(key string, values []string) *URLBuilder {
	for _, value := range values {
		b.query.Add(key, value)
	}
	return b
}

// Fragment sets the URL fragment after the # delimiter.
func (b *URLBuilder) Fragment(fragment string) *URLBuilder {
	b.fragment = fragment
	return b
}

// Build constructs the final URL string.
func (b *URLBuilder) Build() (string, error) {
	u := &url.URL{
		Scheme: b.scheme,
		Host:   b.buildHost(),
		Path:   b.path.String(),
	}

	if len(b.query) > 0 {
		u.RawQuery = b.query.Encode()
	}

	if b.fragment != "" {
		u.Fragment = b.fragment
	}

	return u.String(), nil
}

// String implements fmt.Stringer.
func (b *URLBuilder) String() string {
	result, _ := b.Build()
	return result
}

// Reset restores the builder to its initial state.
func (b *URLBuilder) Reset() *URLBuilder {
	b.scheme = ""
	b.host = ""
	b.port = ""
	b.path.Reset()
	b.query = make(url.Values)
	b.fragment = ""
	return b
}

// Clone creates an independent copy of the builder.
func (b *URLBuilder) Clone() *URLBuilder {
	newBuilder := &URLBuilder{
		scheme:   b.scheme,
		host:     b.host,
		port:     b.port,
		fragment: b.fragment,
		query:    make(url.Values),
	}

	// Copy the path.
	newBuilder.path.WriteString(b.path.String())

	// Deep-copy query parameters.
	for k, v := range b.query {
		newBuilder.query[k] = slices.Clone(v)
	}

	return newBuilder
}

// buildHost constructs the host, including the port.
func (b *URLBuilder) buildHost() string {
	if b.host == "" {
		return ""
	}

	if b.port == "" {
		return b.host
	}

	return b.host + ":" + b.port
}

// BuildHTTP constructs an HTTP URL.
func BuildHTTP(host string, pathSegments ...string) *URLBuilder {
	return NewURLBuilder().Scheme("http").Host(host).AppendPath(pathSegments...)
}

// BuildHTTPS constructs an HTTPS URL.
func BuildHTTPS(host string, pathSegments ...string) *URLBuilder {
	return NewURLBuilder().Scheme("https").Host(host).AppendPath(pathSegments...)
}

// FromURL creates a builder from an existing URL string.
func FromURL(rawURL string) (*URLBuilder, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("解析URL失败: %w", err)
	}

	builder := &URLBuilder{
		scheme:   u.Scheme,
		host:     u.Hostname(),
		port:     u.Port(),
		fragment: u.Fragment,
		query:    u.Query(),
	}

	if u.Path != "" {
		builder.path.WriteString(u.Path)
	}

	return builder, nil
}

// Join joins path segments.
func Join(base string, segments ...string) string {
	if len(segments) == 0 {
		return base
	}

	allPaths := make([]string, 0, len(segments)+1)
	if base != "" {
		allPaths = append(allPaths, base)
	}

	for _, segment := range segments {
		if segment != "" {
			allPaths = append(allPaths, segment)
		}
	}

	return path.Join(allPaths...)
}
