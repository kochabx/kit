package validator

import (
	"errors"
	"slices"
	"strings"
)

// Violation describes one field validation failure without its raw value.
type Violation struct {
	// Field is the short field name selected by WithFieldNameTag; it may be empty for single-value validation.
	Field string
	// Tag is the failed validation constraint, such as "required".
	Tag string
	// Param is the constraint parameter, such as "18" in gte=18, or an empty string when absent.
	Param string
	// Path includes the root struct name, nested fields, and collection indexes, such as Request.users[0].email.
	// Field names follow WithFieldNameTag; the path may be empty for single-value validation.
	Path string
	// Message is the translated, human-readable validation message.
	Message string
}

// Error aggregates field failures from Struct or Var and preserves the underlying error chain.
//
// Extract it with errors.AsType:
//
//	if ve, ok := errors.AsType[*validator.Error](err); ok { … }
type Error struct {
	violations []Violation
	message    string
	cause      error
}

// Error joins validation messages with a semicolon and a space.
func (ve *Error) Error() string {
	return ve.message
}

// Violations returns a copy of the failures in the order reported by the underlying validator.
// Modifying the returned slice does not change Error.
func (ve *Error) Violations() []Violation {
	return slices.Clone(ve.violations)
}

// newError copies failures, joins messages, and preserves the cause for Unwrap.
func newError(violations []Violation, cause error) *Error {
	msgs := make([]string, len(violations))
	for i, v := range violations {
		msgs[i] = v.Message
	}
	return &Error{
		violations: slices.Clone(violations),
		cause:      cause,
		message:    strings.Join(msgs, "; "),
	}
}

// AsError reports whether err's error chain contains an *Error.
func AsError(err error) bool {
	_, ok := errors.AsType[*Error](err)
	return ok
}

// Unwrap returns the cause, which can be inspected with errors.AsType[gv.ValidationErrors].
// The cause can expose raw field values and should not be sent directly in public responses.
func (ve *Error) Unwrap() error {
	return ve.cause
}

// ErrInvalidOption identifies invalid New configuration or registration; use errors.Is to match it.
var ErrInvalidOption = errors.New("invalid validator option")
