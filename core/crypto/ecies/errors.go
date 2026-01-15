package ecies

import "errors"

// Key-related errors
var (
	// ErrInvalidPrivateKey indicates that the private key is invalid or malformed
	ErrInvalidPrivateKey = errors.New("ecies: invalid private key")

	// ErrInvalidPublicKey indicates that the public key is invalid or malformed
	ErrInvalidPublicKey = errors.New("ecies: invalid public key")

	// ErrPrivateKeyEmpty indicates that the private key is nil or empty
	ErrPrivateKeyEmpty = errors.New("ecies: private key is empty")

	// ErrPublicKeyEmpty indicates that the public key is nil or empty
	ErrPublicKeyEmpty = errors.New("ecies: public key is empty")

	// ErrKeyNotOnCurve indicates that the public key point is not on the curve
	ErrKeyNotOnCurve = errors.New("ecies: public key point not on curve")
)

// Encryption/Decryption errors
var (
	// ErrEncryptionFailed indicates a general encryption failure
	ErrEncryptionFailed = errors.New("ecies: encryption failed")

	// ErrDecryptionFailed indicates a general decryption failure
	ErrDecryptionFailed = errors.New("ecies: decryption failed")

	// ErrInvalidCiphertext indicates that the ciphertext is malformed or corrupted
	ErrInvalidCiphertext = errors.New("ecies: invalid ciphertext format")

	// ErrCiphertextTooShort indicates that the ciphertext is shorter than the minimum size
	ErrCiphertextTooShort = errors.New("ecies: ciphertext too short")

	// ErrUnsupportedVersion indicates an unsupported protocol version
	ErrUnsupportedVersion = errors.New("ecies: unsupported protocol version")

	// ErrInvalidDataLength is deprecated, use ErrCiphertextTooShort
	ErrInvalidDataLength = ErrCiphertextTooShort
)

// Key derivation errors
var (
	// ErrKeyDerivationFailed indicates that key derivation failed
	ErrKeyDerivationFailed = errors.New("ecies: key derivation failed")
)

// I/O errors
var (
	// ErrInvalidPEMBlock indicates an invalid PEM block format
	ErrInvalidPEMBlock = errors.New("ecies: invalid PEM block")

	// ErrKeyFileRead indicates a failure to read the key file
	ErrKeyFileRead = errors.New("ecies: failed to read key file")

	// ErrInvalidDecode is deprecated, use ErrInvalidPEMBlock
	ErrInvalidDecode = ErrInvalidPEMBlock
)
