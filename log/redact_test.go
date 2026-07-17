package log

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/kochabx/kit/log/redact"
)

func TestRedactorMasksFieldsAndContent(t *testing.T) {
	r, err := redact.New(
		redact.Field("password", redact.Replace("******")),
		redact.Field("phone", redact.KeepEdges(3, 4)),
		redact.Field("payload", redact.Replace("hidden")),
		redact.Content("phone-content", `1[3-9]\d{9}`, redact.KeepEdges(3, 4)),
	)
	if err != nil {
		t.Fatal(err)
	}

	input := []byte(`{"password":"secret","phone":"13812345678","payload":{"token":"x"},"message":"call 13912345678"}`)
	result, changed := r.Append(nil, input)
	if !changed {
		t.Fatal("expected redaction")
	}
	want := `{"password":"******","phone":"138****5678","payload":"hidden","message":"call 139****5678"}`
	if string(result) != want {
		t.Fatalf("got %s, want %s", result, want)
	}
}

func TestRedactorNoMatchDoesNotAllocateResult(t *testing.T) {
	r, err := redact.New(redact.Field("password", redact.Replace("******")))
	if err != nil {
		t.Fatal(err)
	}
	dst := []byte("prefix")
	result, changed := r.Append(dst, []byte(`{"message":"ok"}`))
	if changed || len(result) != len(dst) {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestRedactorAppendPreservesDestination(t *testing.T) {
	r, err := redact.New(redact.Content("phone", `1[3-9]\d{9}`, redact.KeepEdges(3, 4)))
	if err != nil {
		t.Fatal(err)
	}
	result, changed := r.Append([]byte("prefix:"), []byte("13812345678"))
	if !changed || string(result) != "prefix:138****5678" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestRedactorValidation(t *testing.T) {
	tests := []redact.Rule{
		redact.Field("", redact.Replace("x")),
		redact.Field("secret", nil),
		redact.Content("invalid", "[", redact.Replace("x")),
	}
	for _, rule := range tests {
		if _, err := redact.New(rule); err == nil {
			t.Fatal("expected validation error")
		}
	}
	if _, err := redact.New(
		redact.Field("secret", redact.Replace("x")),
		redact.Field("secret", redact.Replace("y")),
	); err == nil {
		t.Fatal("expected duplicate rule error")
	}
}

func TestBuiltinRules(t *testing.T) {
	r, err := redact.New(redact.BuiltinRules()...)
	if err != nil {
		t.Fatal(err)
	}
	got := r.RedactString(`{"phone":"13812345678","password":"secret","message":"email user@example.com"}`)
	if strings.Contains(got, "13812345678") || strings.Contains(got, "secret") || strings.Contains(got, "user@example.com") {
		t.Fatalf("sensitive data leaked: %s", got)
	}
}

func TestWriterReportsInputLength(t *testing.T) {
	r, _ := redact.New(redact.Content("secret", `secret123`, redact.Replace("***")))
	var output bytes.Buffer
	w := redact.NewWriter(&output, r)
	input := []byte("token=secret123")
	n, err := w.Write(input)
	if err != nil || n != len(input) || output.String() != "token=***" {
		t.Fatalf("n=%d err=%v output=%q", n, err, output.String())
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestWriterRejectsShortWrite(t *testing.T) {
	r, _ := redact.New(redact.Content("secret", `secret`, redact.Replace("***")))
	n, err := redact.NewWriter(shortWriter{}, r).Write([]byte("secret"))
	if n != 0 || err != io.ErrShortWrite {
		t.Fatalf("got (%d, %v)", n, err)
	}
}

func TestLoggerWithRedactor(t *testing.T) {
	r, _ := redact.New(redact.Field("phone", redact.KeepEdges(3, 4)))
	logger := New(WithRedactor(r))
	if logger.Redactor() != r {
		t.Fatal("logger did not retain redactor")
	}
}

func TestLoggerOutputsRedactedLog(t *testing.T) {
	r, err := redact.New(
		redact.Field("phone", redact.KeepEdges(3, 4)),
		redact.Field("password", redact.Replace("******")),
		redact.Content(
			"email",
			`\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`,
			redact.Email(),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	logger := newWithWriter(&output, WithRedactor(r))
	logger.Info().
		Str("phone", "13812345678").
		Str("password", "secret123").
		Msg("contact user@example.com")

	got := output.String()
	t.Logf("redacted log: %s", strings.TrimSpace(got))

	for _, sensitive := range []string{"13812345678", "secret123", "user@example.com"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("sensitive value %q leaked in log: %s", sensitive, got)
		}
	}
	for _, masked := range []string{"138****5678", "******", "u***r@example.com"} {
		if !strings.Contains(got, masked) {
			t.Fatalf("expected masked value %q in log: %s", masked, got)
		}
	}
}

func TestRedactorDynamicRules(t *testing.T) {
	r, err := redact.New()
	if err != nil {
		t.Fatal(err)
	}
	phone := redact.Field("phone", redact.KeepEdges(3, 4))
	if err := r.AddRule(phone); err != nil {
		t.Fatal(err)
	}
	if got := r.RedactString(`{"phone":"13812345678"}`); strings.Contains(got, "13812345678") {
		t.Fatalf("added rule did not take effect: %s", got)
	}
	if !r.DisableRule("phone") {
		t.Fatal("failed to disable rule")
	}
	if r.IsEnabled("phone") {
		t.Fatal("disabled rule reported as enabled")
	}
	if got := r.RedactString(`{"phone":"13812345678"}`); !strings.Contains(got, "13812345678") {
		t.Fatalf("disabled rule still took effect: %s", got)
	}
	if !r.EnableRule("phone") {
		t.Fatal("failed to enable rule")
	}
	if !r.IsEnabled("phone") {
		t.Fatal("enabled rule reported as disabled")
	}
	if !r.RemoveRule("phone") {
		t.Fatal("failed to remove rule")
	}
	if r.HasRules() {
		t.Fatal("removed rule remains active")
	}
}

func TestRedactorConcurrentUpdates(t *testing.T) {
	r, err := redact.New(redact.Field("phone", redact.KeepEdges(3, 4)))
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				_ = r.RedactString(`{"phone":"13812345678"}`)
			}
		}()
	}
	for range 100 {
		r.DisableRule("phone")
		r.EnableRule("phone")
	}
	workers.Wait()
}
