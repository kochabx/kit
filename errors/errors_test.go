package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(401, "unauthorized access")
	if err.Code() != 401 {
		t.Errorf("expected code 401, got %d", err.Code())
	}
	if err.Message() != "unauthorized access" {
		t.Errorf("expected message 'unauthorized access', got %s", err.Message())
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

	metadata := err3.Metadata()
	if metadata["user"] != "john" || metadata["action"] != "login" {
		t.Errorf("metadata not set correctly: %v", metadata)
	}

	t.Logf("Error with metadata: %s", err3.Error())
}

func TestWithCause(t *testing.T) {
	originalErr := errors.New("database connection failed")
	err := New(500, "internal server error").WithCause(originalErr)

	if err.Cause() != originalErr {
		t.Error("cause not set correctly")
	}

	t.Logf("Error with cause: %s", err.Error())
}

func TestErrorChaining(t *testing.T) {
	dbErr := errors.New("connection timeout")
	apiErr := New(503, "service unavailable").
		WithCause(dbErr).
		WithMetadata(map[string]string{"endpoint": "/api/users"})

	t.Logf("Error chain: %s", apiErr.Error())
}

func TestAs(t *testing.T) {
	stdErr := errors.New("standard error")
	if _, ok := As(stdErr); ok {
		t.Error("As should not match standard error")
	}

	existingErr := New(404, "not found")
	sameErr, ok := As(existingErr)

	if !ok {
		t.Error("As should match *Error")
	}
	if existingErr != sameErr {
		t.Error("As should return same instance for *Error")
	}

	wrappedErr := fmt.Errorf("wrapped: %w", existingErr)
	unwrappedErr, ok := As(wrappedErr)
	if !ok {
		t.Error("As should match wrapped *Error")
	}
	if existingErr != unwrappedErr {
		t.Error("As should return wrapped *Error")
	}
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

func TestNewWithMetadataComposition(t *testing.T) {
	metadata := map[string]string{"service": "auth", "version": "v2"}
	err := New(401, "authentication failed").WithMetadata(metadata)

	if err.Code() != 401 {
		t.Errorf("expected code 401, got %d", err.Code())
	}

	resultMetadata := err.Metadata()
	if resultMetadata["service"] != "auth" || resultMetadata["version"] != "v2" {
		t.Errorf("metadata not set correctly: %v", resultMetadata)
	}

	t.Logf("Error with initial metadata: %s", err.Error())
}
