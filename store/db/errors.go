package db

import "errors"

var (
	ErrInvalidConfig = errors.New("invalid config")
	ErrInvalidOption = errors.New("invalid option")
)
