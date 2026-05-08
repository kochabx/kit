package hmac

import "errors"

// Sentinel errors. Use errors.Is to check error categories.
var (
	// ErrEmptySecret indicates that the HMAC secret is empty.
	ErrEmptySecret = errors.New("hmac: secret is empty")

	// ErrEmptySignature indicates that the signature value is empty.
	ErrEmptySignature = errors.New("hmac: signature is empty")

	// ErrInvalidTimestamp indicates that the signature timestamp is non-positive
	// or otherwise malformed.
	ErrInvalidTimestamp = errors.New("hmac: invalid timestamp")

	// ErrSignatureExpired indicates that the signature is older than the configured
	// expiration window.
	ErrSignatureExpired = errors.New("hmac: signature expired")

	// ErrFutureTimestamp indicates that the signature timestamp is further in the
	// future than the allowed clock skew.
	ErrFutureTimestamp = errors.New("hmac: timestamp is in the future")

	// ErrInvalidSignatureEncoding indicates that the signature value cannot be
	// decoded with the configured encoding.
	ErrInvalidSignatureEncoding = errors.New("hmac: invalid signature encoding")

	// ErrSignatureMismatch indicates that the computed signature does not match
	// the provided one.
	ErrSignatureMismatch = errors.New("hmac: signature mismatch")

	// ErrUnsupportedEncoding indicates that an unknown Encoding value was used.
	ErrUnsupportedEncoding = errors.New("hmac: unsupported encoding")
)
