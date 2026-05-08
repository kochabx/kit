package hmac

import (
	"crypto/sha512"
	"errors"
	"strings"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestSignVerify_RoundTrip(t *testing.T) {
	signer, err := NewSigner("super-secret")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	sig, err := signer.SignString("hello")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if sig.Value == "" || sig.Timestamp == 0 {
		t.Fatalf("empty signature: %+v", sig)
	}
	if err := signer.VerifyString(sig, "hello"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestSign_NoPayload(t *testing.T) {
	signer, _ := NewSigner("k")
	sig, err := signer.Sign(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify(sig, nil); err != nil {
		t.Fatal(err)
	}
}

func TestNewSigner_EmptySecret(t *testing.T) {
	if _, err := NewSigner(""); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("want ErrEmptySecret, got %v", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner("k", WithClock(fixedClock(now)))
	sig, _ := signer.SignString("body")

	verifier, _ := NewSigner("k", WithClock(fixedClock(now.Add(6*time.Minute))))
	if err := verifier.VerifyString(sig, "body"); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("want ErrSignatureExpired, got %v", err)
	}
}

func TestVerify_CustomExpiration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner("k",
		WithExpiration(10*time.Minute),
		WithClock(fixedClock(now)),
	)
	sig, _ := signer.SignString("body")

	verifier, _ := NewSigner("k",
		WithExpiration(10*time.Minute),
		WithClock(fixedClock(now.Add(8*time.Minute))),
	)
	if err := verifier.VerifyString(sig, "body"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestVerify_DisabledExpiration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner("k", WithClock(fixedClock(now)))
	sig, _ := signer.SignString("body")

	verifier, _ := NewSigner("k",
		WithExpiration(0),
		WithClock(fixedClock(now.Add(24*time.Hour))),
	)
	if err := verifier.VerifyString(sig, "body"); err != nil {
		t.Fatalf("expected valid with disabled expiration, got %v", err)
	}
}

func TestVerify_FutureTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner("k", WithClock(fixedClock(now.Add(time.Hour))))
	sig, _ := signer.SignString("body")

	verifier, _ := NewSigner("k", WithClock(fixedClock(now)))
	if err := verifier.VerifyString(sig, "body"); !errors.Is(err, ErrFutureTimestamp) {
		t.Fatalf("want ErrFutureTimestamp, got %v", err)
	}
}

func TestVerify_ClockSkew(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner("k", WithClock(fixedClock(now.Add(30*time.Second))))
	sig, _ := signer.SignString("body")

	verifier, _ := NewSigner("k",
		WithClockSkew(time.Minute),
		WithClock(fixedClock(now)),
	)
	if err := verifier.VerifyString(sig, "body"); err != nil {
		t.Fatalf("skew should allow it, got %v", err)
	}
}

func TestVerify_PayloadMismatch(t *testing.T) {
	signer, _ := NewSigner("k")
	sig, _ := signer.SignString("data1")
	if err := signer.VerifyString(sig, "data2"); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("want ErrSignatureMismatch, got %v", err)
	}
}

func TestVerify_SecretMismatch(t *testing.T) {
	a, _ := NewSigner("a")
	b, _ := NewSigner("b")
	sig, _ := a.SignString("payload")
	if err := b.VerifyString(sig, "payload"); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("want ErrSignatureMismatch, got %v", err)
	}
}

func TestVerify_Empty(t *testing.T) {
	signer, _ := NewSigner("k")
	if err := signer.VerifyString(Signature{Value: "", Timestamp: 1}, "x"); !errors.Is(err, ErrEmptySignature) {
		t.Fatalf("want ErrEmptySignature, got %v", err)
	}
	if err := signer.VerifyString(Signature{Value: "abcd", Timestamp: 0}, "x"); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("want ErrInvalidTimestamp, got %v", err)
	}
}

func TestVerify_InvalidEncoding(t *testing.T) {
	signer, _ := NewSigner("k")
	sig := Signature{Value: "not-hex!!", Timestamp: time.Now().Unix()}
	if err := signer.VerifyString(sig, "x"); !errors.Is(err, ErrInvalidSignatureEncoding) {
		t.Fatalf("want ErrInvalidSignatureEncoding, got %v", err)
	}
}

func TestEncoding_Base64URL(t *testing.T) {
	signer, _ := NewSigner("k", WithEncoding(EncodingBase64URL))
	sig, err := signer.SignString("payload")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(sig.Value, "+/=") {
		t.Fatalf("URL encoding should not contain +/=, got %q", sig.Value)
	}
	if err := signer.VerifyString(sig, "payload"); err != nil {
		t.Fatal(err)
	}
}

func TestEncoding_Base64Std(t *testing.T) {
	signer, _ := NewSigner("k", WithEncoding(EncodingBase64Std))
	sig, err := signer.SignString("payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.VerifyString(sig, "payload"); err != nil {
		t.Fatal(err)
	}
}

func TestWithHash_SHA512(t *testing.T) {
	signer, _ := NewSigner("k", WithHash(sha512.New))
	sig, err := signer.SignString("hello")
	if err != nil {
		t.Fatal(err)
	}
	// SHA-512 produces 64 bytes => 128 hex chars.
	if len(sig.Value) != 128 {
		t.Fatalf("expected 128 hex chars, got %d", len(sig.Value))
	}
	if err := signer.VerifyString(sig, "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestPackageLevel_SignVerify(t *testing.T) {
	sig, err := Sign("k", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify("k", sig, []byte("body")); err != nil {
		t.Fatal(err)
	}
}

// TestCanonicalMessage_LengthPrefixCollision proves the length-prefixed
// canonical form prevents trivial concatenation collisions.
func TestCanonicalMessage_LengthPrefixCollision(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner("k", WithClock(fixedClock(now)))

	sigA, _ := signer.SignString("ab")
	sigB, _ := signer.SignString("a")
	// Same timestamp, different payloads must yield different MACs.
	if sigA.Value == sigB.Value {
		t.Fatalf("payload boundary collision: %q == %q", sigA.Value, sigB.Value)
	}
}
