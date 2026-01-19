package ecies

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"math/big"
)

// Encrypt encrypts plaintext using ECIES (Elliptic Curve Integrated Encryption Scheme).
//
// The encryption process:
// 1. Generate an ephemeral key pair
// 2. Perform ECDH with the recipient's public key to derive a shared secret
// 3. Derive an AES encryption key using HKDF
// 4. Encrypt the plaintext using AES-256-GCM
// 5. Return: [version || ephemeral_public_key || nonce || tag || ciphertext]
//
// This implementation uses constant-time ECDH from crypto/ecdh for security.
func Encrypt(recipientPublicKey *PublicKey, plaintext []byte) ([]byte, error) {
	if recipientPublicKey == nil {
		return nil, ErrPublicKeyEmpty
	}

	if len(plaintext) == 0 {
		return nil, fmt.Errorf("%w: plaintext is empty", ErrEncryptionFailed)
	}

	// Generate ephemeral key pair for this encryption
	ephemeralKey, err := GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}
	defer ephemeralKey.Destroy()

	// Derive shared secret using ECDH
	sharedSecret, err := ephemeralKey.Encapsulate(recipientPublicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create AES cipher: %v", ErrEncryptionFailed, err)
	}

	// Create GCM mode with custom nonce size
	aesGCM, err := cipher.NewGCMWithNonceSize(block, AESGCMNonceSize)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create GCM: %v", ErrEncryptionFailed, err)
	}

	// Generate random nonce
	nonce := make([]byte, AESGCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("%w: failed to generate nonce: %v", ErrEncryptionFailed, err)
	}

	// Encrypt plaintext (this appends the authentication tag)
	ciphertextWithTag := aesGCM.Seal(nil, nonce, plaintext, nil)

	// Split ciphertext and tag
	tagOffset := len(ciphertextWithTag) - AESGCMTagSize
	encryptedData := ciphertextWithTag[:tagOffset]
	tag := ciphertextWithTag[tagOffset:]

	// Build final ciphertext structure: [version][ephemeral_pubkey][nonce][tag][data]
	ephemeralPubKey := ephemeralKey.Public().Bytes(false)
	totalSize := 1 + len(ephemeralPubKey) + len(nonce) + len(tag) + len(encryptedData)

	// Use buffer pool for the result
	result := getBuffer(totalSize)
	defer putBuffer(result)
	result = result[:totalSize] // Set the correct length
	offset := 0

	// Version
	result[offset] = CurrentVersion
	offset++

	// Ephemeral public key
	copy(result[offset:], ephemeralPubKey)
	offset += len(ephemeralPubKey)

	// Nonce
	copy(result[offset:], nonce)
	offset += len(nonce)

	// Authentication tag
	copy(result[offset:], tag)
	offset += len(tag)

	// Encrypted data
	copy(result[offset:], encryptedData)

	// Make a copy to return, as the buffer will be returned to pool
	finalResult := make([]byte, totalSize)
	copy(finalResult, result)
	return finalResult, nil
}

// Decrypt decrypts ciphertext that was encrypted using Encrypt.
//
// The decryption process:
// 1. Parse the ciphertext structure
// 2. Extract the ephemeral public key
// 3. Perform ECDH with the private key to derive the shared secret
// 4. Derive the AES decryption key using HKDF
// 5. Decrypt using AES-256-GCM and verify the authentication tag
//
// Returns an error if the ciphertext is invalid or authentication fails.
func Decrypt(privateKey *PrivateKey, ciphertext []byte) ([]byte, error) {
	if privateKey == nil {
		return nil, ErrPrivateKeyEmpty
	}

	if len(ciphertext) < MinCiphertextSize {
		return nil, ErrCiphertextTooShort
	}

	// Parse ciphertext structure
	offset := 0

	// Check version
	version := ciphertext[offset]
	offset++

	if version != CurrentVersion {
		return nil, fmt.Errorf("%w: got version %d, expected %d", ErrUnsupportedVersion, version, CurrentVersion)
	}

	// Extract ephemeral public key
	ephemeralPubKeyBytes := ciphertext[offset : offset+PublicKeyBytes]
	offset += PublicKeyBytes

	// Parse ephemeral public key
	ephemeralPublicKey, err := parsePublicKeyBytes(ephemeralPubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ephemeral public key: %v", ErrInvalidCiphertext, err)
	}

	// Extract nonce
	nonce := ciphertext[offset : offset+AESGCMNonceSize]
	offset += AESGCMNonceSize

	// Extract tag
	tag := ciphertext[offset : offset+AESGCMTagSize]
	offset += AESGCMTagSize

	// Extract encrypted data
	encryptedData := ciphertext[offset:]

	// Derive shared secret
	sharedSecret, err := ephemeralPublicKey.Decapsulate(privateKey)
	if err != nil {
		return nil, fmt.Errorf("%w: key derivation failed: %v", ErrDecryptionFailed, err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create AES cipher: %v", ErrDecryptionFailed, err)
	}

	// Create GCM mode
	aesGCM, err := cipher.NewGCMWithNonceSize(block, AESGCMNonceSize)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create GCM: %v", ErrDecryptionFailed, err)
	}

	// Reconstruct ciphertext with tag for GCM
	// Use buffer pool for temporary ciphertext reconstruction
	ciphertextWithTag := getBuffer(len(encryptedData) + len(tag))
	defer putBuffer(ciphertextWithTag)
	ciphertextWithTag = ciphertextWithTag[:0] // Reset length
	ciphertextWithTag = append(ciphertextWithTag, encryptedData...)
	ciphertextWithTag = append(ciphertextWithTag, tag...)

	// Decrypt and verify
	plaintext, err := aesGCM.Open(nil, nonce, ciphertextWithTag, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: authentication failed or corrupted data: %v", ErrDecryptionFailed, err)
	}

	return plaintext, nil
}

// parsePublicKeyBytes parses a public key from uncompressed bytes.
func parsePublicKeyBytes(pubKeyBytes []byte) (*PublicKey, error) {
	if len(pubKeyBytes) != PublicKeyBytes {
		return nil, ErrInvalidPublicKey
	}

	if pubKeyBytes[0] != UncompressedPointTag {
		return nil, fmt.Errorf("%w: expected uncompressed format", ErrInvalidPublicKey)
	}

	// Use ECDH to parse and validate the public key
	curve := ecdh.P256()
	ecdhKey, err := curve.NewPublicKey(pubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}

	// Extract coordinates for compatibility
	x := new(big.Int).SetBytes(pubKeyBytes[1:33])
	y := new(big.Int).SetBytes(pubKeyBytes[33:65])

	return &PublicKey{
		ecdhKey: ecdhKey,
		curve:   elliptic.P256(),
		x:       x,
		y:       y,
	}, nil
}
