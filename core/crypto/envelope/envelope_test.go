package envelope

import (
	"bytes"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
)

var testKey = bytes.Repeat([]byte{0x42}, KeySize)

func TestRoundTrip(t *testing.T) {
	cipher, err := New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt([]byte("database password"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "database password" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestAADAndTampering(t *testing.T) {
	cipher, _ := New(testKey)
	encrypted, err := cipher.EncryptWithAAD([]byte("secret"), []byte("credential:42"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.DecryptWithAAD(encrypted, []byte("credential:43")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("AAD: %v", err)
	}
	message, _ := base64.RawURLEncoding.DecodeString(encrypted)
	message[len(message)-1] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(message)
	if _, err := cipher.DecryptWithAAD(tampered, []byte("credential:42")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("tamper: %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	oldKey := bytes.Repeat([]byte{1}, KeySize)
	newKey := bytes.Repeat([]byte{2}, KeySize)
	oldCipher, _ := New(oldKey, WithKeyID("v1"))
	oldValue, _ := oldCipher.Encrypt([]byte("secret"))
	rotated, err := New(newKey, WithKeyID("v2"), WithDecryptionKey("v1", oldKey))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := rotated.Decrypt(oldValue)
	if err != nil || string(plaintext) != "secret" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
	newValue, _ := rotated.Encrypt([]byte("new"))
	if _, err := oldCipher.Decrypt(newValue); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("old decrypt new: %v", err)
	}
}

func TestValidationAndLimit(t *testing.T) {
	if _, err := New([]byte("short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("key: %v", err)
	}
	cipher, _ := New(testKey, WithMaxPlaintextSize(3))
	if _, err := cipher.Encrypt([]byte("large")); !errors.Is(err, ErrPlaintextTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := cipher.Decrypt("not base64!"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("ciphertext: %v", err)
	}
}

func TestConcurrentUse(t *testing.T) {
	cipher, _ := New(testKey)
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 50 {
				encrypted, err := cipher.Encrypt([]byte("secret"))
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := cipher.Decrypt(encrypted); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wait.Wait()
}
