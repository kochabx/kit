package errors

import "fmt"

// New creates a structured error with a literal message.
func New(code int, message string) *Error {
	return &Error{code: code, message: message}
}

// Newf creates a structured error with a formatted message.
func Newf(code int, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

// Wrap creates a structured error that unwraps to cause. A nil cause returns nil.
func Wrap(cause error, code int, message string) error {
	if cause == nil {
		return nil
	}
	return &Error{code: code, message: message, cause: cause}
}

// Wrapf creates a structured error with a formatted message and cause.
// A nil cause returns nil.
func Wrapf(cause error, code int, format string, args ...any) error {
	if cause == nil {
		return nil
	}
	return &Error{code: code, message: fmt.Sprintf(format, args...), cause: cause}
}
