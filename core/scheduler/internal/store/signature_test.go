package store

import (
	"bytes"
	"testing"
)

func TestSubmissionSignatureIncludesRawPayloadAndPolicy(t *testing.T) {
	a := EnqueueRecord{ID: "id", TaskName: "task", Payload: []byte{0, 255, 1}, Policy: ExecutionPolicy{MaxExecutions: 10}}
	original := submissionSignature(a)
	b := a
	b.Payload = []byte{0, 255, 2}
	if original == submissionSignature(b) {
		t.Fatal("payload omitted")
	}
	b = a
	b.Policy.MaxExecutions++
	if original == submissionSignature(b) {
		t.Fatal("policy omitted")
	}
	if original != submissionSignature(a) {
		t.Fatal("unstable request signature")
	}
}

func BenchmarkSubmissionSignature(b *testing.B) {
	for _, size := range []int{64, 256 << 10} {
		name := "64B"
		if size > 64 {
			name = "256KiB"
		}
		b.Run(name, func(b *testing.B) {
			r := EnqueueRecord{ID: "id", TaskName: "task", Payload: bytes.Repeat([]byte("x"), size)}
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				_ = submissionSignature(r)
			}
		})
	}
}
