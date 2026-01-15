package mfa

import (
	"strings"
	"testing"
)

func Test(t *testing.T) {
	ga := NewGoogleAuthenticator()
	secret := ga.GenerateSecret()
	code, _ := ga.GenerateCode(secret)
	qr, _ := ga.GenerateQRCode("test@gmail.com", "test", secret)
	ok, _ := ga.ValidateCode(secret, code)

	t.Log(secret, code, qr, ok)
}

func TestGoogleAuthenticator_GenerateSecret(t *testing.T) {
	ga := NewGoogleAuthenticator()

	// Test multiple generations to ensure uniqueness
	secrets := make(map[string]bool)
	for i := 0; i < 100; i++ {
		secret := ga.GenerateSecret()
		if secrets[secret] {
			t.Errorf("Generated duplicate secret: %s", secret)
		}
		secrets[secret] = true

		// Check format
		if len(secret) == 0 {
			t.Error("Generated empty secret")
		}
		if secret != strings.ToUpper(secret) {
			t.Error("Secret should be uppercase")
		}
	}
}

func TestGoogleAuthenticator_ValidateCode_WithTimeWindow(t *testing.T) {
	ga := NewGoogleAuthenticator(WithTimeWindow(2))
	secret := ga.GenerateSecret()

	// Test current time
	code, err := ga.GenerateCode(secret)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}

	valid, err := ga.ValidateCode(secret, code)
	if err != nil {
		t.Fatalf("Failed to validate code: %v", err)
	}
	if !valid {
		t.Error("Code should be valid")
	}
}

func TestGoogleAuthenticator_GenerateQRCode_Validation(t *testing.T) {
	ga := NewGoogleAuthenticator()
	secret := ga.GenerateSecret()

	// Test valid inputs
	qr, err := ga.GenerateQRCode("test@example.com", "TestApp", secret)
	if err != nil {
		t.Fatalf("Failed to generate QR code: %v", err)
	}
	if len(qr) == 0 {
		t.Error("QR code should not be empty")
	}

	// Test empty inputs
	_, err = ga.GenerateQRCode("", "TestApp", secret)
	if err == nil {
		t.Error("Should fail with empty label")
	}

	_, err = ga.GenerateQRCode("test@example.com", "", secret)
	if err == nil {
		t.Error("Should fail with empty issuer")
	}

	_, err = ga.GenerateQRCode("test@example.com", "TestApp", "")
	if err == nil {
		t.Error("Should fail with empty secret")
	}
}

func TestGoogleAuthenticator_Configuration(t *testing.T) {
	ga := NewGoogleAuthenticator(
		WithSecretSize(20),
		WithExpireSecond(60),
		WithDigits(8),
		WithQrCodeSize(512),
		WithTimeWindow(3),
	)

	impl := ga.(*GoogleAuthenticator)
	if impl.SecretSize != 20 {
		t.Errorf("Expected SecretSize 20, got %d", impl.SecretSize)
	}
	if impl.ExpireSecond != 60 {
		t.Errorf("Expected ExpireSecond 60, got %d", impl.ExpireSecond)
	}
	if impl.Digits != 8 {
		t.Errorf("Expected Digits 8, got %d", impl.Digits)
	}
	if impl.QrCodeSize != 512 {
		t.Errorf("Expected QrCodeSize 512, got %d", impl.QrCodeSize)
	}
	if impl.TimeWindow != 3 {
		t.Errorf("Expected TimeWindow 3, got %d", impl.TimeWindow)
	}
}

func TestGoogleAuthenticator_CodeLength(t *testing.T) {
	ga := NewGoogleAuthenticator(WithDigits(8))
	secret := ga.GenerateSecret()

	code, err := ga.GenerateCode(secret)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}

	if len(code) != 8 {
		t.Errorf("Expected code length 8, got %d", len(code))
	}
}

func BenchmarkGenerateSecret(b *testing.B) {
	ga := NewGoogleAuthenticator()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ga.GenerateSecret()
	}
}

func BenchmarkGenerateCode(b *testing.B) {
	ga := NewGoogleAuthenticator()
	secret := ga.GenerateSecret()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ga.GenerateCode(secret)
	}
}

func BenchmarkValidateCode(b *testing.B) {
	ga := NewGoogleAuthenticator()
	secret := ga.GenerateSecret()
	code, _ := ga.GenerateCode(secret)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ga.ValidateCode(secret, code)
	}
}
