package ecies

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

// TestEncryptDecrypt tests basic encryption and decryption functionality
func TestEncryptDecrypt(t *testing.T) {
	// Generate key pair
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	publicKey := privateKey.Public()

	// Test data
	plaintext := []byte("Hello, ECIES!")

	// Encrypt
	ciphertext, err := Encrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	t.Logf("ciphertext: %v", ciphertext)

	// Verify ciphertext is longer than plaintext
	if len(ciphertext) <= len(plaintext) {
		t.Error("Ciphertext should be longer than plaintext")
	}

	// Decrypt
	decrypted, err := Decrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	t.Logf("decrypted: %v", string(decrypted))

	// Verify
	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("Decrypted data doesn't match original.\nExpected: %s\nGot: %s", plaintext, decrypted)
	}
}

// TestEncryptDecryptLargeData tests with larger data sizes
func TestEncryptDecryptLargeData(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	// Test with repeated data
	plaintext := bytes.Repeat([]byte("Hello, world! "), 100) // ~1.4 KB

	ciphertext, err := Encrypt(privateKey.Public(), plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	decrypted, err := Decrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Large data decryption mismatch")
	}
}

// TestEncryptDecryptEmpty tests edge case with empty data
func TestEncryptDecryptEmpty(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	plaintext := []byte{}

	_, err = Encrypt(privateKey.Public(), plaintext)
	if err == nil {
		t.Error("Expected error when encrypting empty data")
	}
}

// TestInvalidCiphertext tests error handling for invalid ciphertext
func TestInvalidCiphertext(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	tests := []struct {
		name       string
		ciphertext []byte
		wantErr    error
	}{
		{
			name:       "too short",
			ciphertext: []byte("short"),
			wantErr:    ErrCiphertextTooShort,
		},
		{
			name:       "empty",
			ciphertext: []byte{},
			wantErr:    ErrCiphertextTooShort,
		},
		{
			name:       "wrong version",
			ciphertext: make([]byte, MinCiphertextSize),
			wantErr:    ErrUnsupportedVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decrypt(privateKey, tt.ciphertext)
			if err == nil {
				t.Error("Expected error for invalid ciphertext")
			}
		})
	}
}

// TestCiphertextTampering tests that tampering is detected
func TestCiphertextTampering(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	plaintext := []byte("Secret message")
	ciphertext, err := Encrypt(privateKey.Public(), plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Tamper with the ciphertext (modify last byte)
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0x01

	// Decryption should fail due to authentication tag mismatch
	_, err = Decrypt(privateKey, tampered)
	if err == nil {
		t.Error("Expected decryption to fail with tampered ciphertext")
	}
}

// TestDifferentKeys tests that different keys produce different ciphertexts
func TestDifferentKeys(t *testing.T) {
	key1, _ := GenerateKey()
	defer key1.Destroy()
	key2, _ := GenerateKey()
	defer key2.Destroy()

	plaintext := []byte("Same message")

	ct1, _ := Encrypt(key1.Public(), plaintext)
	ct2, _ := Encrypt(key2.Public(), plaintext)

	// Different ephemeral keys should produce different ciphertexts
	if bytes.Equal(ct1, ct2) {
		t.Error("Different keys should produce different ciphertexts")
	}

	// Each key should decrypt its own ciphertext
	dec1, err := Decrypt(key1, ct1)
	if err != nil || !bytes.Equal(plaintext, dec1) {
		t.Error("Key1 should decrypt its own ciphertext")
	}

	dec2, err := Decrypt(key2, ct2)
	if err != nil || !bytes.Equal(plaintext, dec2) {
		t.Error("Key2 should decrypt its own ciphertext")
	}

	// Cross-decryption should fail
	_, err = Decrypt(key1, ct2)
	if err == nil {
		t.Error("Key1 should not decrypt key2's ciphertext")
	}
}

// TestRandomness tests that same plaintext produces different ciphertexts
func TestRandomness(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	plaintext := []byte("Same message every time")

	// Encrypt multiple times
	ciphertexts := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		ct, err := Encrypt(privateKey.Public(), plaintext)
		if err != nil {
			t.Fatalf("Encryption %d failed: %v", i, err)
		}
		ciphertexts[i] = ct
	}

	// All ciphertexts should be different (due to random ephemeral keys and nonces)
	for i := 0; i < len(ciphertexts); i++ {
		for j := i + 1; j < len(ciphertexts); j++ {
			if bytes.Equal(ciphertexts[i], ciphertexts[j]) {
				t.Errorf("Ciphertexts %d and %d are identical", i, j)
			}
		}
	}

	// But all should decrypt to the same plaintext
	for i, ct := range ciphertexts {
		dec, err := Decrypt(privateKey, ct)
		if err != nil {
			t.Errorf("Decryption %d failed: %v", i, err)
		}
		if !bytes.Equal(plaintext, dec) {
			t.Errorf("Decryption %d mismatch", i)
		}
	}
}

// TestBase64Encoding tests compatibility with base64 encoding (common use case)
func TestBase64Encoding(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	plaintext := []byte("Message to encode")

	ciphertext, err := Encrypt(privateKey.Public(), plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	// Decode from base64
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Base64 decode failed: %v", err)
	}

	// Decrypt
	decrypted, err := Decrypt(privateKey, decoded)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Base64 roundtrip failed")
	}
}

// TestNilKeys tests error handling for nil keys
func TestNilKeys(t *testing.T) {
	plaintext := []byte("test")

	_, err := Encrypt(nil, plaintext)
	if err != ErrPublicKeyEmpty {
		t.Errorf("Expected ErrPublicKeyEmpty, got %v", err)
	}

	_, err = Decrypt(nil, []byte("fake ciphertext"))
	if err != ErrPrivateKeyEmpty {
		t.Errorf("Expected ErrPrivateKeyEmpty, got %v", err)
	}
}

// TestKeyDestroy tests that Destroy() clears sensitive data
func TestKeyDestroy(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Get original bytes
	originalBytes := privateKey.Bytes()
	if len(originalBytes) == 0 {
		t.Fatal("Key bytes are empty before destroy")
	}

	// Destroy the key
	privateKey.Destroy()

	// Check that D is nil
	if privateKey.d != nil {
		t.Error("D should be nil after Destroy()")
	}

	// Check that Bytes() returns nil
	destroyedBytes := privateKey.Bytes()
	if destroyedBytes != nil {
		t.Error("Bytes() should return nil after Destroy()")
	}
}

// TestVeryLargeData tests with large data (10 MB)
func TestVeryLargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large data test in short mode")
	}

	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	// Generate 10 MB of random data
	plaintext := make([]byte, 10*1024*1024)
	_, err = rand.Read(plaintext)
	if err != nil {
		t.Fatalf("Failed to generate random data: %v", err)
	}

	ciphertext, err := Encrypt(privateKey.Public(), plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	decrypted, err := Decrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Large data decryption mismatch")
	}
}
