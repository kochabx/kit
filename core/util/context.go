package util

import (
	"context"

	"github.com/kochabx/kit/errors"
)

// CtxValue returns the value of the key from the context.
// It returns an error if the context is nil, the value is not found, or the value type is mismatched.
func CtxValue[T any](ctx context.Context, key any) (T, error) {
	var value T
	if ctx == nil {
		return value, errors.Internal("context is nil, key: %v", key)
	}

	val := ctx.Value(key)
	if val == nil {
		return value, errors.Internal("context value not found, key: %v", key)
	}

	if value, ok := val.(T); ok {
		return value, nil
	}

	return value, errors.Internal("context value type mismatch, key: %v, expected: %T, got: %T", key, value, val)
}
