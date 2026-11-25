// Package sign provides anti-replay short-lived HMAC signatures alongside
// Ed25519 and post-quantum ML-DSA (FIPS 204) asymmetric signatures.
package sign

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	MinHMACKeySize    = 32
	NonceSize         = 16
	DefaultExpiration = 5 * time.Minute
	DefaultKeyID      = "default"
	hmacFormatVersion = uint8(1)
	hmacDomain        = "kochabx/kit/hmac:v1"
)

var (
	ErrInvalidKey        = errors.New("invalid signing key")
	ErrUnknownKey        = errors.New("unknown signing key ID")
	ErrInvalidSignature  = errors.New("invalid signature")
	ErrSignatureMismatch = errors.New("signature mismatch")
	ErrSignatureExpired  = errors.New("signature expired")
	ErrFutureTimestamp   = errors.New("signature timestamp is in the future")
	ErrInvalidOption     = errors.New("invalid signing option")
)

// Signature represents a short-lived anti-replay HMAC signature.
type Signature struct {
	Version   uint8  `json:"version"`
	KeyID     string `json:"kid"`
	Timestamp int64  `json:"ts"`
	Nonce     string `json:"nonce"`
	Value     string `json:"sig"`
}

// ReplayKey returns a unique deduplication key bound to key ID, timestamp, and nonce.
func (s *Signature) ReplayKey() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%s", s.KeyID, s.Timestamp, s.Nonce)
}

type hmacConfig struct {
	primaryID  string
	keys       map[string][]byte
	expiration time.Duration
	skew       time.Duration
	clock      func() time.Time
}

// HMACOption customizes HMACSigner construction.
type HMACOption func(*hmacConfig) error

func WithHMACKeyID(kid string) HMACOption {
	return func(c *hmacConfig) error {
		if kid == "" || len(kid) > 255 {
			return fmt.Errorf("%w: invalid key ID", ErrInvalidOption)
		}
		c.primaryID = kid
		return nil
	}
}

func WithHMACVerifyKey(kid string, key []byte) HMACOption {
	return func(c *hmacConfig) error {
		if kid == "" || len(kid) > 255 || len(key) < MinHMACKeySize {
			return fmt.Errorf("%w: verify key %q must be >= %d bytes", ErrInvalidOption, kid, MinHMACKeySize)
		}
		c.keys[kid] = slices.Clone(key)
		return nil
	}
}

// WithExpiration sets signature validity duration (must be >= 1s; sub-second truncation is rejected).
func WithExpiration(d time.Duration) HMACOption {
	return func(c *hmacConfig) error {
		if d < time.Second || d%time.Second != 0 {
			return fmt.Errorf("%w: expiration must be a positive whole number of seconds (>= 1s)", ErrInvalidOption)
		}
		c.expiration = d
		return nil
	}
}

// WithSkew sets tolerated clock skew across hosts (>= 0).
func WithSkew(d time.Duration) HMACOption {
	return func(c *hmacConfig) error {
		if d < 0 {
			return fmt.Errorf("%w: negative clock skew", ErrInvalidOption)
		}
		c.skew = d
		return nil
	}
}

// WithClock overrides the time source (primarily for deterministic testing).
func WithClock(clock func() time.Time) HMACOption {
	return func(c *hmacConfig) error {
		if clock == nil {
			return fmt.Errorf("%w: nil clock", ErrInvalidOption)
		}
		c.clock = clock
		return nil
	}
}

// HMACSigner issues and verifies short-lived lock-free HMAC-SHA256 signatures.
type HMACSigner struct {
	primaryID  string
	keys       map[string][]byte
	expiration time.Duration
	skew       time.Duration
	clock      func() time.Time
}

// NewHMAC constructs an HMACSigner validating key length (>= 32 bytes) and whole-second expiration.
func NewHMAC(primaryKey []byte, opts ...HMACOption) (*HMACSigner, error) {
	if len(primaryKey) < MinHMACKeySize {
		return nil, fmt.Errorf("%w: key must be at least %d bytes", ErrInvalidKey, MinHMACKeySize)
	}
	cfg := hmacConfig{
		primaryID:  DefaultKeyID,
		keys:       make(map[string][]byte),
		expiration: DefaultExpiration,
		clock:      time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidOption)
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if _, conflict := cfg.keys[cfg.primaryID]; conflict {
		return nil, fmt.Errorf("%w: verify key conflicts with primary key ID %q", ErrInvalidOption, cfg.primaryID)
	}
	cfg.keys[cfg.primaryID] = slices.Clone(primaryKey)
	return &HMACSigner{
		primaryID:  cfg.primaryID,
		keys:       cfg.keys,
		expiration: cfg.expiration,
		skew:       cfg.skew,
		clock:      cfg.clock,
	}, nil
}

// ReplayTTL returns the safe anti-replay cache TTL covering signedAt future skew, expiration, and second truncation.
func (s *HMACSigner) ReplayTTL() time.Duration {
	return s.expiration + 2*s.skew + time.Second
}

// Sign signs payload with the primary key, current Unix seconds, and a random 16B nonce.
func (s *HMACSigner) Sign(payload []byte) (*Signature, error) {
	var nonce [NonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	ts := s.clock().Unix()
	mac := computeMAC(s.keys[s.primaryID], s.primaryID, ts, nonce[:], payload)
	return &Signature{
		Version:   hmacFormatVersion,
		KeyID:     s.primaryID,
		Timestamp: ts,
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce[:]),
		Value:     base64.RawURLEncoding.EncodeToString(mac),
	}, nil
}

// Verify checks signature freshness (including skew), key membership, and constant-time MAC.
func (s *HMACSigner) Verify(sig *Signature, payload []byte) error {
	if sig == nil || sig.Version != hmacFormatVersion || sig.KeyID == "" || sig.Nonce == "" || sig.Value == "" {
		return ErrInvalidSignature
	}
	key, ok := s.keys[sig.KeyID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, sig.KeyID)
	}
	nonce, err := base64.RawURLEncoding.DecodeString(sig.Nonce)
	if err != nil || len(nonce) != NonceSize {
		return ErrInvalidSignature
	}
	sigMAC, err := base64.RawURLEncoding.DecodeString(sig.Value)
	if err != nil || len(sigMAC) != sha256.Size {
		return ErrInvalidSignature
	}

	now := s.clock()
	signedAt := time.Unix(sig.Timestamp, 0)
	if signedAt.After(now.Add(s.skew)) {
		return ErrFutureTimestamp
	}
	if now.Sub(signedAt) > s.expiration+s.skew {
		return ErrSignatureExpired
	}

	expected := computeMAC(key, sig.KeyID, sig.Timestamp, nonce, payload)
	if !hmac.Equal(sigMAC, expected) {
		return ErrSignatureMismatch
	}
	return nil
}

func computeMAC(key []byte, kid string, ts int64, nonce, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(hmacDomain))
	mac.Write([]byte{0})
	mac.Write([]byte(kid))
	mac.Write([]byte{0})
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(ts))
	mac.Write(tsBuf[:])
	mac.Write([]byte{0})
	mac.Write(nonce)
	mac.Write([]byte{0})
	mac.Write(payload)
	return mac.Sum(nil)
}
