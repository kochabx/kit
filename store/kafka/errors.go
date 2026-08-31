package kafka

import "errors"

var (
	ErrInvalidConfig = errors.New("invalid config")
	ErrInvalidOption = errors.New("invalid option")
	ErrClosed        = errors.New("client closed")
)
