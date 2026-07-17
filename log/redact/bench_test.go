package redact

import (
	"bytes"
	"testing"
)

var benchInput = []byte(`{"level":"info","time":"2026-03-21 12:00:00","caller":"handler.go:42","message":"user login","phone":"13812345678","password":"secret123","token":"eyJhbGciOiJIUzI1NiJ9.payload.sig","email":"user@example.com","idcard":"110101199003079234"}`)

func benchmarkRedactor(b *testing.B) *Redactor {
	r, err := New(BuiltinRules()...)
	if err != nil {
		b.Fatal(err)
	}
	return r
}

func BenchmarkRedact(b *testing.B) {
	r := benchmarkRedactor(b)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = r.Append(nil, benchInput)
	}
}

func BenchmarkRedactNoMatch(b *testing.B) {
	r := benchmarkRedactor(b)
	input := []byte(`{"level":"info","message":"system started"}`)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = r.Append(nil, input)
	}
}

func BenchmarkWriterNoRules(b *testing.B) {
	r, _ := New()
	var output bytes.Buffer
	w := NewWriter(&output, r)
	b.ReportAllocs()
	for b.Loop() {
		output.Reset()
		_, _ = w.Write(benchInput)
	}
}
