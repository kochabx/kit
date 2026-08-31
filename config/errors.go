package config

import "errors"

var (
	ErrInvalidOptions    = errors.New("invalid options")
	ErrUnsupportedSource = errors.New("unsupported source")
	ErrNotLoaded         = errors.New("not loaded")
	ErrAlreadyWatching   = errors.New("already watching")
	ErrRead              = errors.New("read failed")
	ErrDecode            = errors.New("decode failed")
	ErrValidation        = errors.New("validation failed")
)
