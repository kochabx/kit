package etcd

import "errors"

var (
	ErrInvalidConfig = errors.New("invalid config")
	ErrLockHeld      = errors.New("lock already held")
	ErrServiceExists = errors.New("service already registered")
)
