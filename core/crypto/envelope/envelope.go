// Package envelope provides versioned AES-256-GCM encryption for credentials
// and other small values stored in databases or configuration stores.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	KeySize             = 32
	DefaultKeyID        = "default"
	DefaultMaxPlaintext = 1 << 20
	formatVersion       = 1
	maxKeyIDSize        = 255
)

var (
	magic                = [4]byte{'K', 'E', 'N', 'V'}
	ErrInvalidKey        = errors.New("envelope: invalid key")
	ErrUnknownKey        = errors.New("envelope: unknown key")
	ErrInvalidCiphertext = errors.New("envelope: invalid ciphertext")
	ErrEncryptionFailed  = errors.New("envelope: encryption failed")
	ErrDecryptionFailed  = errors.New("envelope: decryption failed")
	ErrPlaintextTooLarge = errors.New("envelope: plaintext too large")
	ErrInvalidOption     = errors.New("envelope: invalid option")
)

type config struct {
	currentKeyID string
	oldKeys      map[string][]byte
	random       io.Reader
	maxPlaintext int
}

type Option func(*config) error

// WithKeyID sets the public identifier written into new ciphertexts.
func WithKeyID(keyID string) Option {
	return func(config *config) error {
		if keyID == "" || len(keyID) > maxKeyIDSize {
			return fmt.Errorf("%w: invalid key ID", ErrInvalidOption)
		}
		config.currentKeyID = keyID
		return nil
	}
}

// WithDecryptionKey adds an old key for decrypting values during rotation.
// New values always use the key passed to New.
func WithDecryptionKey(keyID string, key []byte) Option {
	return func(config *config) error {
		if keyID == "" || len(keyID) > maxKeyIDSize || len(key) != KeySize {
			return fmt.Errorf("%w: invalid decryption key", ErrInvalidOption)
		}
		if config.oldKeys == nil {
			config.oldKeys = make(map[string][]byte)
		}
		config.oldKeys[keyID] = append([]byte(nil), key...)
		return nil
	}
}

// WithMaxPlaintextSize changes the default 1 MiB safety limit.
func WithMaxPlaintextSize(size int) Option {
	return func(config *config) error {
		if size <= 0 {
			return fmt.Errorf("%w: maximum plaintext size must be positive", ErrInvalidOption)
		}
		config.maxPlaintext = size
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

// Cipher encrypts with the current key and decrypts with current or historical
// keys. It is safe for concurrent use.
type Cipher struct {
	currentKeyID string
	keys         map[string][]byte
	random       io.Reader
	randomMu     sync.Mutex
	maxPlaintext int
}

// New creates a database-value cipher using a 32-byte AES-256 key.
func New(key []byte, options ...Option) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	config := config{
		currentKeyID: DefaultKeyID,
		random:       rand.Reader,
		maxPlaintext: DefaultMaxPlaintext,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: option is nil", ErrInvalidOption)
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	keys := make(map[string][]byte, len(config.oldKeys)+1)
	for keyID, oldKey := range config.oldKeys {
		keys[keyID] = append([]byte(nil), oldKey...)
	}
	keys[config.currentKeyID] = append([]byte(nil), key...)
	return &Cipher{
		currentKeyID: config.currentKeyID,
		keys:         keys,
		random:       config.random,
		maxPlaintext: config.maxPlaintext,
	}, nil
}

// Encrypt encrypts plaintext and returns a URL-safe, self-describing string
// suitable for a database TEXT column.
func (cipher *Cipher) Encrypt(plaintext []byte) (string, error) {
	return cipher.EncryptWithAAD(plaintext, nil)
}

// EncryptWithAAD also authenticates aad without storing or encrypting it.
func (cipher *Cipher) EncryptWithAAD(plaintext, aad []byte) (string, error) {
	if len(plaintext) > cipher.maxPlaintext {
		return "", ErrPlaintextTooLarge
	}
	aead, err := newAEAD(cipher.keys[cipher.currentKeyID])
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrEncryptionFailed, err)
	}
	nonce := make([]byte, aead.NonceSize())
	cipher.randomMu.Lock()
	_, err = io.ReadFull(cipher.random, nonce)
	cipher.randomMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("%w: random nonce: %w", ErrEncryptionFailed, err)
	}
	header := makeHeader(cipher.currentKeyID)
	sealed := aead.Seal(nil, nonce, plaintext, authenticatedData(header, aad))
	message := make([]byte, 0, len(header)+len(nonce)+len(sealed))
	message = append(message, header...)
	message = append(message, nonce...)
	message = append(message, sealed...)
	return base64.RawURLEncoding.EncodeToString(message), nil
}

// Decrypt authenticates and decrypts a value produced by Encrypt.
func (cipher *Cipher) Decrypt(encoded string) ([]byte, error) {
	return cipher.DecryptWithAAD(encoded, nil)
}

// DecryptWithAAD requires the exact aad supplied to EncryptWithAAD.
func (cipher *Cipher) DecryptWithAAD(encoded string, aad []byte) ([]byte, error) {
	maxMessageSize := len(magic) + 2 + maxKeyIDSize + 12 + cipher.maxPlaintext + 16
	if len(encoded) > base64.RawURLEncoding.EncodedLen(maxMessageSize) {
		return nil, ErrInvalidCiphertext
	}
	message, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	keyID, header, nonce, sealed, err := parseMessage(message)
	if err != nil {
		return nil, err
	}
	key, ok := cipher.keys[keyID]
	if !ok {
		return nil, ErrUnknownKey
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecryptionFailed, err)
	}
	if len(nonce) != aead.NonceSize() || len(sealed) < aead.Overhead() || len(sealed)-aead.Overhead() > cipher.maxPlaintext {
		return nil, ErrInvalidCiphertext
	}
	plaintext, err := aead.Open(nil, nonce, sealed, authenticatedData(header, aad))
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func makeHeader(keyID string) []byte {
	header := make([]byte, 0, len(magic)+2+len(keyID))
	header = append(header, magic[:]...)
	header = append(header, formatVersion, byte(len(keyID)))
	header = append(header, keyID...)
	return header
}

func authenticatedData(header, aad []byte) []byte {
	result := make([]byte, 0, len(header)+8+len(aad))
	result = append(result, header...)
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(aad)))
	result = append(result, length[:]...)
	result = append(result, aad...)
	return result
}

func parseMessage(message []byte) (keyID string, header, nonce, sealed []byte, err error) {
	const fixedHeaderSize = 6
	if len(message) < fixedHeaderSize || !equalMagic(message[:4]) || message[4] != formatVersion {
		return "", nil, nil, nil, ErrInvalidCiphertext
	}
	keyIDSize := int(message[5])
	headerSize := fixedHeaderSize + keyIDSize
	nonceSize := 12
	if keyIDSize == 0 || len(message) < headerSize+nonceSize+16 {
		return "", nil, nil, nil, ErrInvalidCiphertext
	}
	return string(message[fixedHeaderSize:headerSize]), message[:headerSize], message[headerSize : headerSize+nonceSize], message[headerSize+nonceSize:], nil
}

func equalMagic(value []byte) bool {
	return len(value) == len(magic) && value[0] == magic[0] && value[1] == magic[1] && value[2] == magic[2] && value[3] == magic[3]
}
