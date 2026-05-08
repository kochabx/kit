// Package hmac provides time-bounded HMAC signatures for API requests and
// other integrity-protected payloads.
//
// The package signs a tuple (timestamp, payload) with HMAC, returning the
// hex/base64 encoded MAC together with the timestamp used. Verification
// recomputes the MAC, performs a constant-time comparison and enforces a
// configurable expiration window.
//
// Canonical message layout (length-prefixed, collision-free):
//
//	uint64-be(timestamp) || uint64-be(len(payload)) || payload
//
// The secret is used only as the HMAC key; it is never mixed into the message.
//
// Example:
//
//	signer, _ := hmac.NewSigner("super-secret")
//	sig, _ := signer.SignString("GET /v1/users")
//	if err := signer.VerifyString(sig, "GET /v1/users"); err != nil {
//	    // handle err: errors.Is(err, hmac.ErrSignatureExpired) ...
//	}
package hmac

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"time"
)

// Encoding controls how the raw MAC bytes are textualised.
type Encoding uint8

const (
	// EncodingHex encodes the MAC as lowercase hexadecimal (default).
	EncodingHex Encoding = iota
	// EncodingBase64Std encodes the MAC using standard base64 (RFC 4648 §4).
	EncodingBase64Std
	// EncodingBase64URL encodes the MAC using URL-safe base64 (RFC 4648 §5),
	// without padding.
	EncodingBase64URL
)

// DefaultExpiration is the default validity window for a signature.
const DefaultExpiration = 5 * time.Minute

// Signature is the result of a Sign call.
type Signature struct {
	// Value is the encoded MAC string (encoding is determined by the Signer).
	Value string
	// Timestamp is the Unix timestamp (seconds) used when computing the MAC.
	Timestamp int64
}

// Signer signs and verifies payloads with a fixed secret and configuration.
// A Signer is safe for concurrent use.
type Signer struct {
	secret     []byte
	hashFn     func() hash.Hash
	expiration time.Duration
	skew       time.Duration
	encoding   Encoding
	clock      func() time.Time
}

// Option configures a Signer.
type Option func(*Signer)

// WithHash sets the underlying hash constructor. Defaults to sha256.New.
func WithHash(fn func() hash.Hash) Option {
	return func(s *Signer) {
		if fn != nil {
			s.hashFn = fn
		}
	}
}

// WithExpiration sets the validity window enforced by Verify. Defaults to
// DefaultExpiration. A non-positive value disables expiration checking.
func WithExpiration(d time.Duration) Option {
	return func(s *Signer) { s.expiration = d }
}

// WithClockSkew permits the verifier to accept timestamps up to d in the
// future, compensating for clock drift. Defaults to 0.
func WithClockSkew(d time.Duration) Option {
	return func(s *Signer) {
		if d >= 0 {
			s.skew = d
		}
	}
}

// WithEncoding selects how the MAC is encoded as text. Defaults to EncodingHex.
func WithEncoding(e Encoding) Option {
	return func(s *Signer) { s.encoding = e }
}

// WithClock injects a clock function, primarily for testing.
func WithClock(fn func() time.Time) Option {
	return func(s *Signer) {
		if fn != nil {
			s.clock = fn
		}
	}
}

// NewSigner creates a Signer with the given secret and options.
// It returns ErrEmptySecret if secret is empty.
func NewSigner(secret string, opts ...Option) (*Signer, error) {
	if secret == "" {
		return nil, ErrEmptySecret
	}
	s := &Signer{
		secret:     []byte(secret),
		hashFn:     sha256.New,
		expiration: DefaultExpiration,
		encoding:   EncodingHex,
		clock:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Sign computes a signature for payload using the current clock timestamp.
func (s *Signer) Sign(payload []byte) (Signature, error) {
	ts := s.clock().Unix()
	mac := s.compute(ts, payload)
	return Signature{Value: s.encode(mac), Timestamp: ts}, nil
}

// SignString is a convenience wrapper around Sign for string payloads.
func (s *Signer) SignString(payload string) (Signature, error) {
	return s.Sign([]byte(payload))
}

// Verify validates sig against payload, checking both authenticity and the
// expiration window.
func (s *Signer) Verify(sig Signature, payload []byte) error {
	if sig.Value == "" {
		return ErrEmptySignature
	}
	if sig.Timestamp <= 0 {
		return ErrInvalidTimestamp
	}

	now := s.clock().Unix()
	skew := int64(s.skew.Seconds())
	if sig.Timestamp-now > skew {
		return ErrFutureTimestamp
	}
	if s.expiration > 0 {
		if now-sig.Timestamp > int64(s.expiration.Seconds()) {
			return ErrSignatureExpired
		}
	}

	got, err := s.decode(sig.Value)
	if err != nil {
		return err
	}
	want := s.compute(sig.Timestamp, payload)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrSignatureMismatch
	}
	return nil
}

// VerifyString is a convenience wrapper around Verify for string payloads.
func (s *Signer) VerifyString(sig Signature, payload string) error {
	return s.Verify(sig, []byte(payload))
}

// compute returns the raw MAC bytes for (timestamp, payload).
func (s *Signer) compute(timestamp int64, payload []byte) []byte {
	h := hmac.New(s.hashFn, s.secret)
	var hdr [16]byte
	binary.BigEndian.PutUint64(hdr[0:8], uint64(timestamp))
	binary.BigEndian.PutUint64(hdr[8:16], uint64(len(payload)))
	h.Write(hdr[:])
	if len(payload) > 0 {
		h.Write(payload)
	}
	return h.Sum(nil)
}

func (s *Signer) encode(b []byte) string {
	switch s.encoding {
	case EncodingBase64Std:
		return base64.StdEncoding.EncodeToString(b)
	case EncodingBase64URL:
		return base64.RawURLEncoding.EncodeToString(b)
	default:
		return hex.EncodeToString(b)
	}
}

func (s *Signer) decode(v string) ([]byte, error) {
	var (
		out []byte
		err error
	)
	switch s.encoding {
	case EncodingHex:
		out, err = hex.DecodeString(v)
	case EncodingBase64Std:
		out, err = base64.StdEncoding.DecodeString(v)
	case EncodingBase64URL:
		out, err = base64.RawURLEncoding.DecodeString(v)
	default:
		return nil, ErrUnsupportedEncoding
	}
	if err != nil {
		return nil, ErrInvalidSignatureEncoding
	}
	return out, nil
}

// Sign is a package-level convenience that signs payload using a transient Signer.
func Sign(secret string, payload []byte, opts ...Option) (Signature, error) {
	s, err := NewSigner(secret, opts...)
	if err != nil {
		return Signature{}, err
	}
	return s.Sign(payload)
}

// Verify is a package-level convenience that verifies sig with a transient Signer.
func Verify(secret string, sig Signature, payload []byte, opts ...Option) error {
	s, err := NewSigner(secret, opts...)
	if err != nil {
		return err
	}
	return s.Verify(sig, payload)
}
