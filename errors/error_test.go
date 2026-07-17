package errors

import (
	stderrors "errors"
	"fmt"
	"reflect"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(401, "unauthorized").WithMetadata(map[string]string{"method": "GET", "path": "/api/resource"})
	want := "code=401, message=unauthorized, metadata={method=GET, path=/api/resource}"
	if err.Code() != 401 || err.Message() != "unauthorized" || err.Error() != want {
		t.Fatalf("unexpected error: code=%d message=%q text=%q", err.Code(), err.Message(), err.Error())
	}
}

func TestNewf(t *testing.T) {
	err := Newf(404, "user %d not found", 42)
	if err.Message() != "user 42 not found" {
		t.Fatalf("unexpected message: %q", err.Message())
	}
}

func TestWrap(t *testing.T) {
	cause := stderrors.New("connection refused")
	err := Wrap(cause, 503, "database unavailable")
	if err.Error() != "code=503, message=database unavailable, cause=connection refused" {
		t.Fatalf("unexpected error text: %q", err.Error())
	}
	if !stderrors.Is(err, cause) {
		t.Fatal("wrapped cause not found by errors.Is")
	}
	if Wrap(nil, 500, "ignored") != nil {
		t.Fatal("wrapping nil must return nil")
	}
	var nilWrapped error = Wrap(nil, 500, "ignored")
	if nilWrapped != nil {
		t.Fatal("wrapping nil produced a non-nil error interface")
	}
}

func TestFrom(t *testing.T) {
	structured := New(409, "conflict")
	wrapped := fmt.Errorf("request failed: %w", structured)
	got, ok := From(wrapped)
	if !ok || got != structured {
		t.Fatalf("From() = (%v, %v)", got, ok)
	}
	if _, ok := From(stderrors.New("plain")); ok {
		t.Fatal("plain error matched structured Error")
	}
}

func TestMetadataIsImmutable(t *testing.T) {
	base := New(400, "invalid input")
	metadata := map[string]string{"field": "email"}
	err := base.WithMetadata(metadata).With("reason", "invalid")
	metadata["field"] = "password"

	want := map[string]string{"field": "email", "reason": "invalid"}
	if got := err.Metadata(); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %v, want %v", got, want)
	}
	copy := err.Metadata()
	copy["field"] = "changed"
	if got := err.Metadata()["field"]; got != "email" {
		t.Fatalf("metadata mutated through returned map: %q", got)
	}
	if base.Metadata() != nil {
		t.Fatal("base error was mutated")
	}
}

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		_ = New(500, "internal server error")
	}
}

func BenchmarkWrap(b *testing.B) {
	cause := stderrors.New("database error")
	for b.Loop() {
		_ = Wrap(cause, 500, "internal server error")
	}
}

func BenchmarkError(b *testing.B) {
	err := Wrap(stderrors.New("database error"), 500, "internal server error")
	for b.Loop() {
		_ = err.Error()
	}
}
