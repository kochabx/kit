package errors

import (
	stderrors "errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
)

const (
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
	code     int
	message  string
	metadata map[string]string
	cause    error
}

// Error returns a human-readable error message with optional error chain
func (e *Error) Error() string {
	var msg strings.Builder

	msg.WriteString("code=")
	msg.WriteString(strconv.Itoa(e.code))
	msg.WriteString(MetadataSeparator)
	msg.WriteString("message=")
	msg.WriteString(e.message)

	if len(e.metadata) > 0 {
		msg.WriteString(MetadataSeparator)
		msg.WriteString(MetadataPrefix)
		keys := make([]string, 0, len(e.metadata))
		for k := range e.metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				msg.WriteString(", ")
			}
			msg.WriteString(k)
			msg.WriteByte('=')
			msg.WriteString(e.metadata[k])
		}
		msg.WriteString(MetadataSuffix)
	}

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

// WithMetadata returns a copy of the error with metadata merged in.
func (e *Error) WithMetadata(m map[string]string) *Error {
	if len(m) == 0 {
		return e
	}

	err := e.clone()
	if err.metadata == nil {
		err.metadata = make(map[string]string, len(m))
	}

	maps.Copy(err.metadata, m)
	return err
}

// WithCause returns a copy of the error with cause attached.
func (e *Error) WithCause(cause error) *Error {
	if cause == nil {
		return e
	}

	err := e.clone()
	err.cause = cause
	return err
}

// clone creates a shallow copy of the error while deep copying metadata.
func (e *Error) clone() *Error {
	var metadata map[string]string
	if len(e.metadata) > 0 {
		metadata = make(map[string]string, len(e.metadata))
		maps.Copy(metadata, e.metadata)
	}

	return &Error{
		code:     e.code,
		message:  e.message,
		metadata: metadata,
		cause:    e.cause,
	}
}

// Is reports whether err is an *Error with the same error code and message.
// This implements the standard errors.Is interface for better error comparison
func (e *Error) Is(err error) bool {
	var ge *Error
	if stderrors.As(err, &ge) {
		return e.code == ge.code && e.message == ge.message
	}
	return false
}

// Code returns the error code.
func (e *Error) Code() int {
	return e.code
}

// Message returns the error message.
func (e *Error) Message() string {
	return e.message
}

// Metadata returns a copy of the metadata.
func (e *Error) Metadata() map[string]string {
	if len(e.metadata) == 0 {
		return nil
	}

	result := make(map[string]string, len(e.metadata))
	maps.Copy(result, e.metadata)
	return result
}

// Cause returns the underlying cause.
func (e *Error) Cause() error {
	return e.cause
}

// Status returns a copy of the structured error status.
func (e *Error) Status() Status {
	return Status{
		Code:     e.code,
		Message:  e.message,
		Metadata: e.Metadata(),
	}
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
		code:    code,
		message: message,
	}
}

// As reports whether err contains a structured *Error.
func As(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}

	var ge *Error
	if stderrors.As(err, &ge) {
		return ge, true
	}

	return nil, false
}
