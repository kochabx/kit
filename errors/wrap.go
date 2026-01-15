package errors

import (
	goerrors "errors"
)

// Unwrap returns the result of calling the Unwrap method on err,
// if err's type contains an Unwrap method returning error.
// Otherwise, Unwrap returns nil.
func Unwrap(err error) error {
	return goerrors.Unwrap(err)
}

// Is reports whether any error in err's chain matches target.
func Is(err, target error) bool {
	return goerrors.Is(err, target)
}

// As finds the first error in err's chain that matches target,
// and if so, sets target to that error value and returns true.
// Otherwise, it returns false.
func As(err error, target any) bool {
	return goerrors.As(err, target)
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
func Join(errs ...error) error {
	return goerrors.Join(errs...)
}
