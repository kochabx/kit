package mongo

import "errors"

var (
	ErrNotInitialized   = errors.New("mongo: not initialized")
	ErrConnectionFailed = errors.New("failed to connect to mongodb")
)
