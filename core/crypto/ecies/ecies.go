// Package ecies provides a misuse-resistant facade over the Go standard
// library's RFC 9180 Hybrid Public Key Encryption (HPKE) implementation.
//
// Despite the historical package name, this is not a bespoke ECIES scheme.
// HPKE standardises the same hybrid-encryption pattern, including its KDF,
// domain separation, authenticated encryption, and wire-level operations.
package ecies

import (
	"crypto/ecdh"
	"crypto/hpke"
	"errors"
	"fmt"
)

var (
	ErrInvalidKey       = errors.New("ecies: invalid key")
	ErrInvalidMessage   = errors.New("ecies: invalid message")
	ErrEncryptionFailed = errors.New("ecies: encryption failed")
	ErrDecryptionFailed = errors.New("ecies: decryption failed")
)

// Suite is an immutable HPKE cipher suite and is safe for concurrent use.
type Suite struct {
	kem  hpke.KEM
	kdf  hpke.KDF
	aead hpke.AEAD
}

// X25519ChaCha20 returns the recommended general-purpose RFC 9180 suite:
// DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, and ChaCha20-Poly1305.
func X25519ChaCha20() Suite {
	return Suite{hpke.DHKEM(ecdh.X25519()), hpke.HKDFSHA256(), hpke.ChaCha20Poly1305()}
}

// HybridPostQuantum returns a hybrid post-quantum suite combining ML-KEM-768
// and X25519. It trades larger keys and messages for post-quantum protection.
func HybridPostQuantum() Suite {
	return Suite{hpke.MLKEM768X25519(), hpke.SHAKE128(), hpke.AES256GCM()}
}

type PrivateKey struct{ key hpke.PrivateKey }
type PublicKey struct{ key hpke.PublicKey }

// GenerateKey creates a key pair for suite.
func (suite Suite) GenerateKey() (*PrivateKey, error) {
	if !suite.valid() {
		return nil, ErrInvalidKey
	}
	key, err := suite.kem.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("%w: generate: %w", ErrInvalidKey, err)
	}
	return &PrivateKey{key}, nil
}

// ParsePrivateKey parses an RFC 9180 SerializePrivateKey value.
func (suite Suite) ParsePrivateKey(encoded []byte) (*PrivateKey, error) {
	if !suite.valid() || len(encoded) == 0 {
		return nil, ErrInvalidKey
	}
	key, err := suite.kem.NewPrivateKey(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: private key: %w", ErrInvalidKey, err)
	}
	return &PrivateKey{key}, nil
}

// ParsePublicKey parses an RFC 9180 SerializePublicKey value.
func (suite Suite) ParsePublicKey(encoded []byte) (*PublicKey, error) {
	if !suite.valid() || len(encoded) == 0 {
		return nil, ErrInvalidKey
	}
	key, err := suite.kem.NewPublicKey(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: public key: %w", ErrInvalidKey, err)
	}
	return &PublicKey{key}, nil
}

// Bytes returns a fresh RFC 9180 serialization of the private key.
func (key *PrivateKey) Bytes() ([]byte, error) {
	if key == nil || key.key == nil {
		return nil, ErrInvalidKey
	}
	encoded, err := key.key.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: serialize private key: %w", ErrInvalidKey, err)
	}
	return encoded, nil
}

func (key *PrivateKey) Public() *PublicKey {
	if key == nil || key.key == nil {
		return nil
	}
	return &PublicKey{key.key.PublicKey()}
}

// Bytes returns a fresh RFC 9180 serialization of the public key.
func (key *PublicKey) Bytes() []byte {
	if key == nil || key.key == nil {
		return nil
	}
	return key.key.Bytes()
}

// Message maps directly to the RFC 9180 enc and ct values. Keeping the values
// separate avoids inventing a package-specific binary framing protocol.
type Message struct {
	Encapsulation []byte `json:"enc"`
	Ciphertext    []byte `json:"ct"`
}

// Seal encrypts one message. info provides protocol domain separation; aad
// authenticates record metadata without encrypting it. Use a stable, unique
// info value for every application protocol.
func (suite Suite) Seal(recipient *PublicKey, info, aad, plaintext []byte) (*Message, error) {
	if !suite.valid() || recipient == nil || recipient.key == nil || recipient.key.KEM().ID() != suite.kem.ID() {
		return nil, ErrInvalidKey
	}
	enc, sender, err := hpke.NewSender(recipient.key, suite.kdf, suite.aead, info)
	if err != nil {
		return nil, fmt.Errorf("%w: setup: %w", ErrEncryptionFailed, err)
	}
	ciphertext, err := sender.Seal(aad, plaintext)
	if err != nil {
		return nil, fmt.Errorf("%w: seal: %w", ErrEncryptionFailed, err)
	}
	return &Message{Encapsulation: enc, Ciphertext: ciphertext}, nil
}

// Open authenticates and decrypts one message. info and aad must exactly match
// the values supplied to Seal.
func (suite Suite) Open(recipient *PrivateKey, info, aad []byte, message *Message) ([]byte, error) {
	if !suite.valid() || recipient == nil || recipient.key == nil || recipient.key.KEM().ID() != suite.kem.ID() {
		return nil, ErrInvalidKey
	}
	if message == nil || len(message.Encapsulation) == 0 || len(message.Ciphertext) == 0 {
		return nil, ErrInvalidMessage
	}
	receiver, err := hpke.NewRecipient(message.Encapsulation, recipient.key, suite.kdf, suite.aead, info)
	if err != nil {
		return nil, fmt.Errorf("%w: setup: %w", ErrDecryptionFailed, err)
	}
	plaintext, err := receiver.Open(aad, message.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %w", ErrDecryptionFailed, err)
	}
	return plaintext, nil
}

func (suite Suite) valid() bool {
	return suite.kem != nil && suite.kdf != nil && suite.aead != nil
}
