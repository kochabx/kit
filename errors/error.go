// Package errors provides structured errors with a numeric code, safe message,
// metadata, and standard Go error chaining.
package errors

import (
	stderrors "errors"
	"maps"
	"sort"
	"strconv"
	"strings"
)

const separator = ", "

// Error is an immutable structured error.
type Error struct {
	code     int
	message  string
	metadata map[string]string
	cause    error
}

// Error returns a deterministic structured text representation.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	var text strings.Builder
	text.Grow(len(e.message) + 32)
	text.WriteString("code=")
	text.WriteString(strconv.Itoa(e.code))
	text.WriteString(separator)
	text.WriteString("message=")
	text.WriteString(e.message)

	if len(e.metadata) > 0 {
		keys := make([]string, 0, len(e.metadata))
		for key := range e.metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		text.WriteString(separator)
		text.WriteString("metadata={")
		for i, key := range keys {
			if i > 0 {
				text.WriteString(separator)
			}
			text.WriteString(key)
			text.WriteByte('=')
			text.WriteString(e.metadata[key])
		}
		text.WriteByte('}')
	}

	if e.cause != nil {
		text.WriteString(separator)
		text.WriteString("cause=")
		text.WriteString(e.cause.Error())
	}

	return text.String()
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Code returns the application error code.
func (e *Error) Code() int {
	if e == nil {
		return 0
	}
	return e.code
}

// Message returns the safe, caller-facing message without the cause.
func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

// Metadata returns a defensive copy of the metadata.
func (e *Error) Metadata() map[string]string {
	if e == nil || len(e.metadata) == 0 {
		return nil
	}
	metadata := make(map[string]string, len(e.metadata))
	maps.Copy(metadata, e.metadata)
	return metadata
}

// WithMetadata returns an error copy with metadata merged into it.
func (e *Error) WithMetadata(metadata map[string]string) *Error {
	if e == nil || len(metadata) == 0 {
		return e
	}
	clone := *e
	clone.metadata = e.Metadata()
	if clone.metadata == nil {
		clone.metadata = make(map[string]string, len(metadata))
	}
	maps.Copy(clone.metadata, metadata)
	return &clone
}

// With returns an error copy containing one metadata entry.
func (e *Error) With(key, value string) *Error {
	if e == nil || key == "" {
		return e
	}
	clone := *e
	clone.metadata = e.Metadata()
	if clone.metadata == nil {
		clone.metadata = make(map[string]string, 1)
	}
	clone.metadata[key] = value
	return &clone
}

// From finds the first structured Error in err's chain.
func From(err error) (*Error, bool) {
	var structured *Error
	if !stderrors.As(err, &structured) {
		return nil, false
	}
	return structured, true
}
