package hmac

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

var testKey = bytes.Repeat([]byte{0x42}, MinKeySize)

func newTestSigner(t *testing.T, now time.Time, current string, keys map[string][]byte) *Signer {
	t.Helper()
	options := []Option{
		WithKeyID(current),
		WithClock(func() time.Time { return now }),
		WithRandomReader(bytes.NewReader(bytes.Repeat([]byte{1}, 4096))),
	}
	for keyID, key := range keys {
		if keyID != current {
			options = append(options, WithVerificationKey(keyID, key))
		}
	}
	signer, err := New(keys[current], options...)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer := newTestSigner(t, now, "v2", map[string][]byte{"v2": testKey})
	signature, err := signer.Sign([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify(signature, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if signature.KeyID != "v2" || signature.ReplayKey() == "" {
		t.Fatalf("signature = %+v", signature)
	}
}

func TestAuthenticationAndTime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer := newTestSigner(t, now, "v1", map[string][]byte{"v1": testKey})
	signature, err := signer.Sign([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := signature
	tampered.Nonce = "AAAAAAAAAAAAAAAAAAAAAA"
	if err := signer.Verify(tampered, []byte("payload")); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("tamper: %v", err)
	}
	if err := signer.Verify(signature, []byte("other")); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("payload: %v", err)
	}

	expired, _ := New(testKey, WithKeyID("v1"), WithClock(func() time.Time { return now.Add(6 * time.Minute) }))
	if err := expired.Verify(signature, []byte("payload")); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("expired: %v", err)
	}
	future, _ := New(testKey, WithKeyID("v1"), WithClock(func() time.Time { return now.Add(-time.Minute) }))
	if err := future.Verify(signature, []byte("payload")); !errors.Is(err, ErrFutureTimestamp) {
		t.Fatalf("future: %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	oldKey := bytes.Repeat([]byte{1}, MinKeySize)
	newKey := bytes.Repeat([]byte{2}, MinKeySize)
	oldSigner := newTestSigner(t, now, "old", map[string][]byte{"old": oldKey})
	oldSignature, _ := oldSigner.Sign([]byte("payload"))
	rotated := newTestSigner(t, now, "new", map[string][]byte{"old": oldKey, "new": newKey})
	if err := rotated.Verify(oldSignature, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	newSignature, _ := rotated.Sign([]byte("payload"))
	if newSignature.KeyID != "new" {
		t.Fatalf("KeyID = %q", newSignature.KeyID)
	}
}

func TestValidation(t *testing.T) {
	if _, err := New([]byte("short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("short key: %v", err)
	}
	if _, err := New(testKey, WithExpiration(0)); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("expiration: %v", err)
	}
}

func TestConcurrentUse(t *testing.T) {
	signer, err := New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 50 {
				signature, err := signer.Sign([]byte("payload"))
				if err != nil {
					t.Error(err)
					return
				}
				if err := signer.Verify(signature, []byte("payload")); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wait.Wait()
}
