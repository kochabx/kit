// Package hmac implements short-lived HMAC-SHA256 message signatures with
// random nonces and key rotation support. Callers use Signature.ReplayKey with
// shared state when replay prevention is required.
package hmac

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	MinKeySize        = 32
	NonceSize         = 16
	DefaultExpiration = 5 * time.Minute
	DefaultKeyID      = "default"
	formatVersion     = 1
)

var (
	ErrInvalidKey        = errors.New("hmac: invalid key")
	ErrUnknownKey        = errors.New("hmac: unknown key")
	ErrInvalidSignature  = errors.New("hmac: invalid signature")
	ErrSignatureMismatch = errors.New("hmac: signature mismatch")
	ErrSignatureExpired  = errors.New("hmac: signature expired")
	ErrFutureTimestamp   = errors.New("hmac: timestamp is in the future")
	ErrNonceGeneration   = errors.New("hmac: nonce generation failed")
	ErrInvalidOption     = errors.New("hmac: invalid option")
)

// Signature is safe to serialize as JSON. Value and Nonce use unpadded
// base64url encoding. KeyID selects the verification key during rotation.
type Signature struct {
	KeyID     string `json:"kid"`
	Timestamp int64  `json:"ts"`
	Nonce     string `json:"nonce"`
	Value     string `json:"sig"`
}

type config struct {
	currentKeyID     string
	verificationKeys map[string][]byte
	expiration       time.Duration
	skew             time.Duration
	clock            func() time.Time
	random           io.Reader
}

type Option func(*config) error

// WithKeyID sets the public identifier placed in new signatures.
func WithKeyID(keyID string) Option {
	return func(config *config) error {
		if keyID == "" || len(keyID) > 255 {
			return fmt.Errorf("%w: invalid key ID", ErrInvalidOption)
		}
		config.currentKeyID = keyID
		return nil
	}
}

// WithVerificationKey adds an old key that remains valid during rotation.
// New signatures always use the key passed to New.
func WithVerificationKey(keyID string, key []byte) Option {
	return func(config *config) error {
		if keyID == "" || len(keyID) > 255 || len(key) < MinKeySize {
			return fmt.Errorf("%w: invalid verification key", ErrInvalidOption)
		}
		if config.verificationKeys == nil {
			config.verificationKeys = make(map[string][]byte)
		}
		config.verificationKeys[keyID] = append([]byte(nil), key...)
		return nil
	}
}

func WithExpiration(expiration time.Duration) Option {
	return func(config *config) error {
		if expiration <= 0 {
			return fmt.Errorf("%w: expiration must be positive", ErrInvalidOption)
		}
		config.expiration = expiration
		return nil
	}
}

func WithClockSkew(skew time.Duration) Option {
	return func(config *config) error {
		if skew < 0 {
			return fmt.Errorf("%w: clock skew must not be negative", ErrInvalidOption)
		}
		config.skew = skew
		return nil
	}
}

// WithClock is intended for deterministic tests.
func WithClock(clock func() time.Time) Option {
	return func(config *config) error {
		if clock == nil {
			return fmt.Errorf("%w: clock is nil", ErrInvalidOption)
		}
		config.clock = clock
		return nil
	}
}

// WithRandomReader is intended for deterministic tests.
func WithRandomReader(random io.Reader) Option {
	return func(config *config) error {
		if random == nil {
			return fmt.Errorf("%w: random reader is nil", ErrInvalidOption)
		}
		config.random = random
		return nil
	}
}

// Signer signs with the current key and verifies with every configured key.
// It is immutable after construction and safe for concurrent use.
type Signer struct {
	currentKeyID string
	keys         map[string][]byte
	expiration   time.Duration
	skew         time.Duration
	clock        func() time.Time
	random       io.Reader
	randomMu     sync.Mutex
}

// New creates a signer from a key containing at least 256 bits of entropy.
// With no options it uses safe defaults and is ready for normal use.
func New(key []byte, options ...Option) (*Signer, error) {
	if len(key) < MinKeySize {
		return nil, ErrInvalidKey
	}
	config := config{
		currentKeyID: DefaultKeyID,
		expiration:   DefaultExpiration,
		clock:        time.Now,
		random:       rand.Reader,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: option is nil", ErrInvalidOption)
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	keys := make(map[string][]byte, len(config.verificationKeys)+1)
	for keyID, verificationKey := range config.verificationKeys {
		keys[keyID] = append([]byte(nil), verificationKey...)
	}
	keys[config.currentKeyID] = append([]byte(nil), key...)
	return &Signer{
		currentKeyID: config.currentKeyID,
		keys:         keys,
		expiration:   config.expiration,
		skew:         config.skew,
		clock:        config.clock,
		random:       config.random,
	}, nil
}

// Sign creates a new signature with a cryptographically random nonce.
func (signer *Signer) Sign(payload []byte) (Signature, error) {
	nonce := make([]byte, NonceSize)
	signer.randomMu.Lock()
	_, err := io.ReadFull(signer.random, nonce)
	signer.randomMu.Unlock()
	if err != nil {
		return Signature{}, fmt.Errorf("%w: %w", ErrNonceGeneration, err)
	}
	timestamp := signer.clock().Unix()
	mac := signer.compute(signer.currentKeyID, signer.keys[signer.currentKeyID], timestamp, nonce, payload)
	return Signature{
		KeyID:     signer.currentKeyID,
		Timestamp: timestamp,
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
		Value:     base64.RawURLEncoding.EncodeToString(mac),
	}, nil
}

// Verify authenticates signature and enforces its validity window. Replay
// detection is deliberately handled by the caller because it requires shared
// state across service instances.
func (signer *Signer) Verify(signature Signature, payload []byte) error {
	if signature.KeyID == "" || signature.Timestamp <= 0 || signature.Nonce == "" || signature.Value == "" {
		return ErrInvalidSignature
	}
	key, ok := signer.keys[signature.KeyID]
	if !ok {
		return ErrUnknownKey
	}
	now := signer.clock().Unix()
	if signature.Timestamp-now > int64(signer.skew/time.Second) {
		return ErrFutureTimestamp
	}
	if now-signature.Timestamp > int64(signer.expiration/time.Second) {
		return ErrSignatureExpired
	}
	nonce, err := base64.RawURLEncoding.DecodeString(signature.Nonce)
	if err != nil || len(nonce) != NonceSize {
		return ErrInvalidSignature
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature.Value)
	if err != nil || len(provided) != sha256.Size {
		return ErrInvalidSignature
	}
	expected := signer.compute(signature.KeyID, key, signature.Timestamp, nonce, payload)
	if !hmac.Equal(provided, expected) {
		return ErrSignatureMismatch
	}
	return nil
}

// ReplayKey returns a stable, non-secret key suitable for a replay store.
func (signature Signature) ReplayKey() string {
	return signature.KeyID + ":" + signature.Nonce
}

// Expiration returns the configured signature validity window.
func (signer *Signer) Expiration() time.Duration { return signer.expiration }

func (signer *Signer) compute(keyID string, key []byte, timestamp int64, nonce, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte{formatVersion, byte(len(keyID))})
	mac.Write([]byte(keyID))
	var header [20]byte
	binary.BigEndian.PutUint64(header[0:8], uint64(timestamp))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(nonce)))
	binary.BigEndian.PutUint64(header[12:20], uint64(len(payload)))
	mac.Write(header[:])
	mac.Write(nonce)
	mac.Write(payload)
	return mac.Sum(nil)
}
