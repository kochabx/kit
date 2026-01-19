package cache

import "errors"

var (
	// ErrSessionNotFound 会话未找到
	ErrSessionNotFound = errors.New("cache: session not found")

	// ErrCacheNotFound 缓存未找到
	ErrCacheNotFound = errors.New("cache: cache not found")
)
