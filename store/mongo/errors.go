package mongo

import "errors"

// ErrInvalidConfig reports invalid or inconsistent Config values.
var ErrInvalidConfig = errors.New("invalid config")
