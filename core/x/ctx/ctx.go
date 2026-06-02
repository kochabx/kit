// Package ctx 提供 context 相关的工具函数。
package ctx

import (
	"context"

	"github.com/kochabx/kit/errors"
)

// Value 从 context 中按 key 取出类型为 T 的值。
// 当 ctx 为 nil、key 不存在或类型不匹配时返回错误。
func Value[T any](ctx context.Context, key any) (T, error) {
	var zero T
	if ctx == nil {
		return zero, errors.Internal("context is nil, key: %v", key)
	}

	val := ctx.Value(key)
	if val == nil {
		return zero, errors.Internal("context value not found, key: %v", key)
	}

	if v, ok := val.(T); ok {
		return v, nil
	}

	return zero, errors.Internal("context value type mismatch, key: %v, expected: %T, got: %T", key, zero, val)
}
