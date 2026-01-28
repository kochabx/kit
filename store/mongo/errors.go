package mongo

import "errors"

var (
	ErrConnectionFailed = errors.New("failed to connect to mongodb")
)
