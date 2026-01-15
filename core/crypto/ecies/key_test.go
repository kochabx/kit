package ecies

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestGenerateKey tests key pair generation
func TestGenerateKey(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	if privateKey == nil {
		t.Fatal("Generated private key is nil")
	}

	if privateKey.Public() == nil {
		t.Fatal("Public key is nil")
	}

	// Check that key bytes are not empty
	privBytes := privateKey.Bytes()
	if len(privBytes) == 0 {
		t.Error("Private key bytes are empty")
	}

	pubBytes := privateKey.Public().Bytes(false)
	if len(pubBytes) != PublicKeyBytes {
		t.Errorf("Public key bytes length = %d, want %d", len(pubBytes), PublicKeyBytes)
	}
}

// TestKeyEquality tests key equality checking
func TestKeyEquality(t *testing.T) {
	key1, _ := GenerateKey()
	defer key1.Destroy()
	key2, _ := GenerateKey()
	defer key2.Destroy()

	// Same key should equal itself
	if !key1.Equals(key1) {
		t.Error("Key should equal itself")
	}

	// Different keys should not be equal
	if key1.Equals(key2) {
		t.Error("Different keys should not be equal")
	}

	// Nil keys
	if key1.Equals(nil) {
		t.Error("Key should not equal nil")
	}

	// Public key equality
	pub1 := key1.Public()
	pub2 := key2.Public()

	if !pub1.Equals(pub1) {
		t.Error("Public key should equal itself")
	}

	if pub1.Equals(pub2) {
		t.Error("Different public keys should not be equal")
	}
}

// TestPublicKeyCompression tests compressed and uncompressed formats
func TestPublicKeyCompression(t *testing.T) {
	privateKey, _ := GenerateKey()
	defer privateKey.Destroy()

	publicKey := privateKey.Public()

	// Uncompressed format
	uncompressed := publicKey.Bytes(false)
	if len(uncompressed) != PublicKeyBytes {
		t.Errorf("Uncompressed length = %d, want %d", len(uncompressed), PublicKeyBytes)
	}
	if uncompressed[0] != UncompressedPointTag {
		t.Errorf("Uncompressed tag = 0x%02x, want 0x%02x", uncompressed[0], UncompressedPointTag)
	}

	// Compressed format
	compressed := publicKey.Bytes(true)
	expectedLen := 1 + CurvePointSize // 33 bytes
	if len(compressed) != expectedLen {
		t.Errorf("Compressed length = %d, want %d", len(compressed), expectedLen)
	}

	tag := compressed[0]
	if tag != CompressedEvenTag && tag != CompressedOddTag {
		t.Errorf("Invalid compressed tag: 0x%02x", tag)
	}
}

// TestSaveLoadPrivateKey tests private key file I/O
func TestSaveLoadPrivateKey(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test_private.pem")

	// Generate and save key
	originalKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	defer originalKey.Destroy()

	err = SavePrivateKey(originalKey, keyPath)
	if err != nil {
		t.Fatalf("Failed to save private key: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatal("Private key file was not created")
	}

	// Load key
	loadedKey, err := LoadPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("Failed to load private key: %v", err)
	}
	defer loadedKey.Destroy()

	// Compare keys
	if !originalKey.Equals(loadedKey) {
		t.Error("Loaded key doesn't match original")
	}

	// Test that loaded key can decrypt data encrypted with original key
	plaintext := []byte("Test message")
	ciphertext, _ := Encrypt(originalKey.Public(), plaintext)
	decrypted, err := Decrypt(loadedKey, ciphertext)
	if err != nil {
		t.Fatalf("Decryption with loaded key failed: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Loaded key cannot properly decrypt")
	}
}

// TestSaveLoadPublicKey tests public key file I/O
func TestSaveLoadPublicKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test_public.pem")

	privateKey, _ := GenerateKey()
	defer privateKey.Destroy()
	originalPubKey := privateKey.Public()

	// Save public key
	err := SavePublicKey(originalPubKey, keyPath)
	if err != nil {
		t.Fatalf("Failed to save public key: %v", err)
	}

	// Load public key
	loadedPubKey, err := LoadPublicKey(keyPath)
	if err != nil {
		t.Fatalf("Failed to load public key: %v", err)
	}

	// Compare
	if !originalPubKey.Equals(loadedPubKey) {
		t.Error("Loaded public key doesn't match original")
	}

	// Test encryption with loaded public key
	plaintext := []byte("Test message")
	ciphertext, err := Encrypt(loadedPubKey, plaintext)
	if err != nil {
		t.Fatalf("Encryption with loaded public key failed: %v", err)
	}

	decrypted, err := Decrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Loaded public key encryption/decryption failed")
	}
}

// TestGenerateKeyPair tests key pair generation and saving
func TestGenerateKeyPair(t *testing.T) {
	tmpDir := t.TempDir()

	err := GenerateKeyPair(
		WithDirpath(tmpDir),
		WithPrivateKeyFilename("my_private.pem"),
		WithPublicKeyFilename("my_public.pem"),
	)
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	// Verify files exist
	privPath := filepath.Join(tmpDir, "my_private.pem")
	pubPath := filepath.Join(tmpDir, "my_public.pem")

	if _, err := os.Stat(privPath); os.IsNotExist(err) {
		t.Error("Private key file not created")
	}
	if _, err := os.Stat(pubPath); os.IsNotExist(err) {
		t.Error("Public key file not created")
	}

	// Load and test keys
	privateKey, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("Failed to load generated private key: %v", err)
	}
	defer privateKey.Destroy()

	publicKey, err := LoadPublicKey(pubPath)
	if err != nil {
		t.Fatalf("Failed to load generated public key: %v", err)
	}

	// Test encryption/decryption
	plaintext := []byte("Test message")
	ciphertext, _ := Encrypt(publicKey, plaintext)
	decrypted, err := Decrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Generated key pair encryption/decryption failed")
	}
}

// TestLoadInvalidFiles tests error handling for invalid files
func TestLoadInvalidFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Test non-existent file
	_, err := LoadPrivateKey(filepath.Join(tmpDir, "nonexistent.pem"))
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test invalid PEM file
	invalidPath := filepath.Join(tmpDir, "invalid.pem")
	os.WriteFile(invalidPath, []byte("not a valid PEM file"), 0600)

	_, err = LoadPrivateKey(invalidPath)
	if err == nil {
		t.Error("Expected error for invalid PEM file")
	}

	_, err = LoadPublicKey(invalidPath)
	if err == nil {
		t.Error("Expected error for invalid PEM file")
	}
}

// TestKeyHex tests hexadecimal encoding
func TestKeyHex(t *testing.T) {
	privateKey, _ := GenerateKey()
	defer privateKey.Destroy()

	privHex := privateKey.Hex()
	if len(privHex) == 0 {
		t.Error("Private key hex is empty")
	}

	pubHex := privateKey.Public().Hex(false)
	if len(pubHex) == 0 {
		t.Error("Public key hex is empty")
	}

	// Hex should be twice the length of bytes (2 hex chars per byte)
	privBytes := privateKey.Bytes()
	if len(privHex) != len(privBytes)*2 {
		t.Errorf("Hex length = %d, expected %d", len(privHex), len(privBytes)*2)
	}
}

// TestSaveNilKeys tests error handling for nil keys
func TestSaveNilKeys(t *testing.T) {
	tmpDir := t.TempDir()

	err := SavePrivateKey(nil, filepath.Join(tmpDir, "test.pem"))
	if err != ErrPrivateKeyEmpty {
		t.Errorf("Expected ErrPrivateKeyEmpty, got %v", err)
	}

	err = SavePublicKey(nil, filepath.Join(tmpDir, "test.pem"))
	if err != ErrPublicKeyEmpty {
		t.Errorf("Expected ErrPublicKeyEmpty, got %v", err)
	}
}
