package sign

import (
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func randomHMACKey(t testing.TB) []byte {
	t.Helper()
	k := make([]byte, MinHMACKeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestHMACReplayTTLSkewSubSecondAndRotation(t *testing.T) {
	if _, err := NewHMAC(randomHMACKey(t), WithExpiration(500*time.Millisecond)); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("sub-second expiration must be rejected, got %v", err)
	}

	now := atomic.Pointer[time.Time]{}
	base := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	now.Store(&base)

	oldKey := randomHMACKey(t)
	newKey := randomHMACKey(t)
	signer, err := NewHMAC(
		newKey,
		WithHMACKeyID("k2"),
		WithHMACVerifyKey("k1", oldKey),
		WithExpiration(10*time.Second),
		WithSkew(2*time.Second),
		WithClock(func() time.Time { return *now.Load() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := signer.ReplayTTL(), 15*time.Second; got != want {
		t.Fatalf("ReplayTTL() = %v, want %v (expiration+2*skew+1s)", got, want)
	}

	payload := []byte(`{"order_id":"ord-1"}`)
	sig, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	if sig.ReplayKey() == "" {
		t.Fatal("empty ReplayKey")
	}
	raw, err := json.Marshal(sig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Signature
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify(&decoded, payload); err != nil {
		t.Fatalf("Verify valid sig failed: %v", err)
	}
	if err := signer.Verify(&decoded, []byte(`tampered`)); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch, got %v", err)
	}

	// Within expiration + skew (11s <= 12s) succeeds; past 12s fails as expired.
	t11 := base.Add(11 * time.Second)
	now.Store(&t11)
	if err := signer.Verify(&decoded, payload); err != nil {
		t.Fatalf("Verify within skew failed: %v", err)
	}
	t13 := base.Add(13 * time.Second)
	now.Store(&t13)
	if err := signer.Verify(&decoded, payload); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("expected ErrSignatureExpired, got %v", err)
	}

	// Verify old rotated key k1 is accepted cleanly.
	oldSigner, err := NewHMAC(oldKey, WithHMACKeyID("k1"), WithClock(func() time.Time { return base }))
	if err != nil {
		t.Fatal(err)
	}
	now.Store(&base)
	oldSig, err := oldSigner.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify(oldSig, payload); err != nil {
		t.Fatalf("Verify old rotated key failed: %v", err)
	}
}

func TestEd25519AndMLDSASignatures(t *testing.T) {
	pub, priv, err := GenerateEd25519()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("release-manifest-v2")
	edSig, err := SignEd25519(priv, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyEd25519(pub, msg, edSig); err != nil {
		t.Fatalf("VerifyEd25519 failed: %v", err)
	}
	if err := VerifyEd25519(pub, []byte("other"), edSig); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch, got %v", err)
	}

	sk, err := GenerateMLDSA(MLDSA65)
	if err != nil {
		t.Fatalf("GenerateMLDSA failed: %v", err)
	}
	parsedSK, err := ParseMLDSAPrivateKey(MLDSA65, sk.Bytes())
	if err != nil {
		t.Fatalf("ParseMLDSAPrivateKey failed: %v", err)
	}
	parsedPK, err := ParseMLDSAPublicKey(MLDSA65, sk.PublicKey().Bytes())
	if err != nil {
		t.Fatalf("ParseMLDSAPublicKey failed: %v", err)
	}

	pqSig, err := SignMLDSA(parsedSK, msg, "kit-auth")
	if err != nil {
		t.Fatalf("SignMLDSA failed: %v", err)
	}
	if err := VerifyMLDSA(parsedPK, msg, pqSig, "kit-auth"); err != nil {
		t.Fatalf("VerifyMLDSA failed: %v", err)
	}
	if err := VerifyMLDSA(parsedPK, msg, pqSig, "wrong-context"); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected context mismatch error, got %v", err)
	}
}
