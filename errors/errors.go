package errors

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
)

const (
	UnknownCode       = 500
	MetadataSeparator = ", "
	MetadataPrefix    = "metadata={"
	MetadataSuffix    = "}"
	CausePrefix       = "cause="
)

// Status represents the status information of an error, including error code, message and metadata
type Status struct {
	Code     int               `json:"code,omitempty"`
	Message  string            `json:"message,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Error represents a structured error containing HTTP status code, message, metadata and error chain
type Error struct {
	Status
	cause error
}

// Error returns a human-readable error message with optional error chain
// Performance optimized through pre-computed buffer size and efficient string building
func (e *Error) Error() string {
	var msg strings.Builder

	// Efficiently build error message
	msg.WriteString("code=")
	msg.WriteString(strconv.Itoa(e.Code))
	msg.WriteString(MetadataSeparator)
	msg.WriteString("message=")
	msg.WriteString(e.Message)

	// Append metadata if present
	if len(e.Metadata) > 0 {
		msg.WriteString(MetadataSeparator)
		msg.WriteString(MetadataPrefix)
		first := true
		for k, v := range e.Metadata {
			if !first {
				msg.WriteString(", ")
			}
			msg.WriteString(k)
			msg.WriteByte('=')
			msg.WriteString(v)
			first = false
		}
		msg.WriteString(MetadataSuffix)
	}

	// Append error cause if present
	if e.cause != nil {
		msg.WriteString(MetadataSeparator)
		msg.WriteString(CausePrefix)
		msg.WriteString(e.cause.Error())
	}

	return msg.String()
}

// Unwrap returns the cause of the error
func (e *Error) Unwrap() error {
	return e.cause
}

// WithMetadata adds metadata to the error. Returns a new error instance to maintain immutability.
func (e *Error) WithMetadata(m map[string]string) *Error {
	if len(m) == 0 {
		return e
	}

	// Clone only when metadata actually needs to be added
	err := e.clone()
	if err.Metadata == nil {
		err.Metadata = make(map[string]string, len(m))
	}

	maps.Copy(err.Metadata, m)
	return err
}

// WithCause adds a cause to the error. Returns a new error instance to maintain immutability.
func (e *Error) WithCause(cause error) *Error {
	if cause == nil {
		return e
	}

	err := e.clone()
	err.cause = cause
	return err
}

// clone creates a shallow copy of the error while deep copying the metadata map
func (e *Error) clone() *Error {
	var metadata map[string]string
	if len(e.Metadata) > 0 {
		metadata = make(map[string]string, len(e.Metadata))
		maps.Copy(metadata, e.Metadata)
	}

	return &Error{
		Status: Status{
			Code:     e.Code,
			Message:  e.Message,
			Metadata: metadata,
		},
		cause: e.cause,
	}
}

// Is reports whether err is an *Error with the same error code and message.
// This implements the standard errors.Is interface for better error comparison
func (e *Error) Is(err error) bool {
	var ge *Error
	if errors.As(err, &ge) {
		return e.Code == ge.Code && e.Message == ge.Message
	}
	return false
}

// GetCode returns the error code
func (e *Error) GetCode() int {
	return e.Code
}

// GetMessage returns the error message
func (e *Error) GetMessage() string {
	return e.Message
}

// GetMetadata returns a copy of the metadata to prevent external modification
func (e *Error) GetMetadata() map[string]string {
	if len(e.Metadata) == 0 {
		return nil
	}

	result := make(map[string]string, len(e.Metadata))
	maps.Copy(result, e.Metadata)
	return result
}

// GetCause returns the underlying cause of the error
func (e *Error) GetCause() error {
	return e.cause
}

// New creates a new error with the given error code and formatted message
func New(code int, format string, args ...any) *Error {
	var message string
	if len(args) == 0 {
		message = format
	} else {
		message = fmt.Sprintf(format, args...)
	}

	return &Error{
		Status: Status{
			Code:    code,
			Message: message,
		},
	}
}

// NewWithMetadata creates a new error with metadata
func NewWithMetadata(code int, metadata map[string]string, format string, args ...any) *Error {
	err := New(code, format, args...)
	if len(metadata) > 0 {
		err.Metadata = make(map[string]string, len(metadata))
		maps.Copy(err.Metadata, metadata)
	}
	return err
}

// FromError converts a generic error to *Error.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}

	// Direct type assertion is more efficient than errors.As for this use case
	if ge, ok := err.(*Error); ok {
		return ge
	}

	return New(UnknownCode, "%v", err)
}

// Wrap wraps an error with additional context while preserving the original error chain
// Returns nil if the input error is nil
func Wrap(err error, code int, format string, args ...any) *Error {
	if err == nil {
		return nil
	}

	newErr := New(code, format, args...)
	return newErr.WithCause(err)
}

// WrapWithMetadata wraps an error with metadata and additional context
// Returns nil if the input error is nil
func WrapWithMetadata(err error, code int, metadata map[string]string, format string, args ...any) *Error {
	if err == nil {
		return nil
	}

	newErr := NewWithMetadata(code, metadata, format, args...)
	return newErr.WithCause(err)
}
