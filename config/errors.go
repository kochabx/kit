package config

import "errors"

var (
	ErrInvalidOptions  = errors.New("invalid options")
	ErrNotLoaded       = errors.New("not loaded")
	ErrAlreadyWatching = errors.New("already watching")
	ErrClosed          = errors.New("config store is closed")
	ErrRead            = errors.New("read failed")
	ErrDecode          = errors.New("decode failed")
	ErrValidation      = errors.New("validation failed")
)
