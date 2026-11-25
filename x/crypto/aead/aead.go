// Package aead provides key-rotated, per-message HKDF-derived AES-256-GCM encryption.
package aead

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
)

const (
	KeySize                   = 32
	SaltSize                  = 16
	nonceSize                 = 12
	DefaultKeyID              = "default"
	DefaultMaxPlaintext       = 1 << 20
	formatVersion       uint8 = 2
	maxKeyIDLen               = 255
)

var (
	magic                = [4]byte{'K', 'E', 'N', '2'}
	ErrInvalidKey        = errors.New("invalid AEAD key")
	ErrUnknownKey        = errors.New("unknown AEAD key ID")
	ErrInvalidCiphertext = errors.New("invalid AEAD ciphertext")
	ErrEncryptionFailed  = errors.New("AEAD encryption failed")
	ErrDecryptionFailed  = errors.New("AEAD decryption failed")
	ErrPlaintextTooLarge = errors.New("plaintext too large")
	ErrInvalidOption     = errors.New("invalid AEAD option")
)

type config struct {
	primaryID    string
	keys         map[string][]byte
	maxPlaintext int
}

// Option customizes Cipher construction.
type Option func(*config) error

// WithKeyID sets the key ID written into newly encrypted ciphertexts (default "default").
func WithKeyID(kid string) Option {
	return func(c *config) error {
		if kid == "" || len(kid) > maxKeyIDLen {
			return fmt.Errorf("%w: invalid key ID", ErrInvalidOption)
		}
		c.primaryID = kid
		return nil
	}
}

// WithDecryptionKey registers an older key for decrypting rotated ciphertexts.
func WithDecryptionKey(kid string, key []byte) Option {
	return func(c *config) error {
		if kid == "" || len(kid) > maxKeyIDLen || len(key) != KeySize {
			return fmt.Errorf("%w: key %q must be %d bytes", ErrInvalidOption, kid, KeySize)
		}
		c.keys[kid] = slices.Clone(key)
		return nil
	}
}

// WithMaxPlaintext sets the maximum allowed plaintext size per Encrypt call.
func WithMaxPlaintext(limit int) Option {
	return func(c *config) error {
		if limit <= 0 {
			return fmt.Errorf("%w: maxPlaintext must be positive", ErrInvalidOption)
		}
		c.maxPlaintext = limit
		return nil
	}
}

// Cipher performs lock-free authenticated encryption using per-message HKDF subkeys.
type Cipher struct {
	primaryID    string
	keys         map[string][]byte // immutable cloned master keys built at New()
	maxPlaintext int
}

// New constructs a Cipher with primaryKey for encrypting new messages and any
// rotated decryption keys provided via WithDecryptionKey.
func New(primaryKey []byte, opts ...Option) (*Cipher, error) {
	if len(primaryKey) != KeySize {
		return nil, fmt.Errorf("%w: key must be %d bytes", ErrInvalidKey, KeySize)
	}
	cfg := config{
		primaryID:    DefaultKeyID,
		keys:         make(map[string][]byte),
		maxPlaintext: DefaultMaxPlaintext,
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
		return nil, fmt.Errorf("%w: decryption key conflicts with primary key ID %q", ErrInvalidOption, cfg.primaryID)
	}
	cfg.keys[cfg.primaryID] = slices.Clone(primaryKey)

	return &Cipher{
		primaryID:    cfg.primaryID,
		keys:         maps.Clone(cfg.keys),
		maxPlaintext: cfg.maxPlaintext,
	}, nil
}

// Encrypt encrypts plaintext using a fresh per-message HKDF key derived from a
// 16-byte random salt, authenticating header metadata alongside aad.
func (c *Cipher) Encrypt(plaintext, aad []byte) ([]byte, error) {
	if len(plaintext) > c.maxPlaintext {
		return nil, fmt.Errorf("%w: %d > %d", ErrPlaintextTooLarge, len(plaintext), c.maxPlaintext)
	}
	primaryKey := c.keys[c.primaryID]

	hdrLen := 4 + 1 + 1 + len(c.primaryID) + SaltSize
	hdrAndNonce := make([]byte, hdrLen+nonceSize)
	copy(hdrAndNonce[:4], magic[:])
	hdrAndNonce[4] = formatVersion
	hdrAndNonce[5] = byte(len(c.primaryID))
	copy(hdrAndNonce[6:], c.primaryID)

	saltOffset := 6 + len(c.primaryID)
	if _, err := rand.Read(hdrAndNonce[saltOffset:]); err != nil {
		return nil, fmt.Errorf("%w: entropy: %w", ErrEncryptionFailed, err)
	}

	salt := hdrAndNonce[saltOffset : saltOffset+SaltSize]
	nonce := hdrAndNonce[hdrLen:]
	gcm, err := deriveGCM(primaryKey, salt, c.primaryID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEncryptionFailed, err)
	}

	header := hdrAndNonce[:hdrLen]
	authData := combineAAD(header, aad)
	sealed := gcm.Seal(hdrAndNonce, nonce, plaintext, authData)
	return sealed, nil
}

// Decrypt authenticates and decrypts an envelope produced by Encrypt.
func (c *Cipher) Decrypt(envelope, aad []byte) ([]byte, error) {
	header, kid, salt, nonce, ct, err := parseEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	masterKey, ok := c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKey, kid)
	}
	gcm, err := deriveGCM(masterKey, salt, kid)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecryptionFailed, err)
	}
	pt, err := gcm.Open(nil, nonce, ct, combineAAD(header, aad))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecryptionFailed, err)
	}
	return pt, nil
}

// EncryptString returns an unpadded base64url encoding of Encrypt.
func (c *Cipher) EncryptString(plaintext, aad []byte) (string, error) {
	b, err := c.Encrypt(plaintext, aad)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecryptString decodes and decrypts an unpadded base64url envelope.
func (c *Cipher) DecryptString(encoded string, aad []byte) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: base64: %w", ErrInvalidCiphertext, err)
	}
	return c.Decrypt(raw, aad)
}

// KeyID returns the key ID encoded inside envelope without decrypting.
func (c *Cipher) KeyID(envelope []byte) (string, error) {
	_, kid, _, _, _, err := parseEnvelope(envelope)
	return kid, err
}

// NeedsReencrypt reports whether envelope was sealed under a non-primary key ID.
func (c *Cipher) NeedsReencrypt(envelope []byte) (bool, error) {
	kid, err := c.KeyID(envelope)
	if err != nil {
		return false, err
	}
	return kid != c.primaryID, nil
}

// Reencrypt decrypts envelope and re-seals it under the current primary key and fresh salt.
func (c *Cipher) Reencrypt(envelope, aad []byte) ([]byte, error) {
	pt, err := c.Decrypt(envelope, aad)
	if err != nil {
		return nil, err
	}
	return c.Encrypt(pt, aad)
}

func deriveGCM(masterKey, salt []byte, kid string) (cipher.AEAD, error) {
	subKey, err := hkdf.Key(sha256.New, masterKey, salt, "kochabx/kit/aead:"+kid, KeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(subKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func combineAAD(header, aad []byte) []byte {
	if len(aad) == 0 {
		return header
	}
	out := make([]byte, len(header)+len(aad))
	copy(out, header)
	copy(out[len(header):], aad)
	return out
}

func parseEnvelope(envelope []byte) (header []byte, kid string, salt, nonce, ct []byte, err error) {
	if len(envelope) < 6+1+SaltSize+nonceSize+16 || [4]byte(envelope[:4]) != magic || envelope[4] != formatVersion {
		return nil, "", nil, nil, nil, ErrInvalidCiphertext
	}
	kidLen := int(envelope[5])
	hdrLen := 6 + kidLen + SaltSize
	if kidLen == 0 || len(envelope) < hdrLen+nonceSize+16 {
		return nil, "", nil, nil, nil, ErrInvalidCiphertext
	}
	kidEnd := 6 + kidLen
	return envelope[:hdrLen],
		string(envelope[6:kidEnd]),
		envelope[kidEnd:hdrLen],
		envelope[hdrLen : hdrLen+nonceSize],
		envelope[hdrLen+nonceSize:],
		nil
}
