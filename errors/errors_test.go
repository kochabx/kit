package errors

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(401, "unauthorized access")
	if err.GetCode() != 401 {
		t.Errorf("expected code 401, got %d", err.GetCode())
	}
	if err.GetMessage() != "unauthorized access" {
		t.Errorf("expected message 'unauthorized access', got %s", err.GetMessage())
	}

	t.Logf("Error: %s", err.Error())
}

func TestWithMetadata(t *testing.T) {
	err := New(401, "unauthorized")

	// Test with empty metadata (should return same instance)
	err2 := err.WithMetadata(map[string]string{})
	if err != err2 {
		t.Error("WithMetadata with empty map should return same instance")
	}

	// Test with actual metadata
	err3 := err.WithMetadata(map[string]string{"user": "john", "action": "login"})
	if err == err3 {
		t.Error("WithMetadata should return new instance")
	}

	metadata := err3.GetMetadata()
	if metadata["user"] != "john" || metadata["action"] != "login" {
		t.Errorf("metadata not set correctly: %v", metadata)
	}

	t.Logf("Error with metadata: %s", err3.Error())
}

func TestWithCause(t *testing.T) {
	originalErr := errors.New("database connection failed")
	err := New(500, "internal server error").WithCause(originalErr)

	if err.GetCause() != originalErr {
		t.Error("cause not set correctly")
	}

	t.Logf("Error with cause: %s", err.Error())
}

func TestErrorChaining(t *testing.T) {
	dbErr := errors.New("connection timeout")
	serviceErr := Wrap(dbErr, 503, "service unavailable")
	apiErr := serviceErr.WithMetadata(map[string]string{"endpoint": "/api/users"})

	t.Logf("Error chain: %s", apiErr.Error())
}

func TestFromError(t *testing.T) {
	// Test with standard error
	stdErr := errors.New("standard error")
	wrappedErr := FromError(stdErr)

	if wrappedErr.GetCode() != UnknownCode {
		t.Errorf("expected code %d, got %d", UnknownCode, wrappedErr.GetCode())
	}

	// Test with existing Error (should return same instance)
	existingErr := New(404, "not found")
	sameErr := FromError(existingErr)

	if existingErr != sameErr {
		t.Error("FromError should return same instance for *Error")
	}

	t.Logf("From standard error: %s", wrappedErr.Error())
}

// Benchmark tests to verify performance improvements
func BenchmarkNewError(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(500, "internal server error")
	}
}

func BenchmarkNewErrorWithFormat(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(500, "error processing request %d", i)
	}
}

func BenchmarkErrorString(b *testing.B) {
	err := New(500, "internal server error").
		WithMetadata(map[string]string{"service": "api", "version": "v1"}).
		WithCause(errors.New("database error"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

func BenchmarkWithMetadata(b *testing.B) {
	err := New(500, "internal server error")
	metadata := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.WithMetadata(metadata)
	}
}

func TestNewWithMetadata(t *testing.T) {
	metadata := map[string]string{"service": "auth", "version": "v2"}
	err := NewWithMetadata(401, metadata, "authentication failed")

	if err.GetCode() != 401 {
		t.Errorf("expected code 401, got %d", err.GetCode())
	}

	resultMetadata := err.GetMetadata()
	if resultMetadata["service"] != "auth" || resultMetadata["version"] != "v2" {
		t.Errorf("metadata not set correctly: %v", resultMetadata)
	}

	t.Logf("Error with initial metadata: %s", err.Error())
}
