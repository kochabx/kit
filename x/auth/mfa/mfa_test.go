package mfa_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kochabx/kit/fault"
	"github.com/kochabx/kit/x/auth/mfa"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
)

func TestTOTPGenerateVerifyAndReplayProtection(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000010, 0).UTC()
	verifier, err := mfa.NewTOTP(mfa.TOTPConfig{
		Issuer: "KitApp",
		Skew:   1,
		Clock:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewTOTP failed: %v", err)
	}

	enr, err := verifier.Generate("alice@example.com")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if enr.Secret == "" || !strings.HasPrefix(enr.URI, "otpauth://totp/") {
		t.Fatalf("unexpected enrollment: %+v", enr)
	}

	code, err := totp.GenerateCode(enr.Secret, now)
	if err != nil {
		t.Fatalf("totp.GenerateCode failed: %v", err)
	}

	// First verification succeeds.
	if err := verifier.Verify(t.Context(), "alice@example.com", enr.Secret, code); err != nil {
		t.Fatalf("first Verify failed: %v", err)
	}

	// Immediate replay within the same step MUST fail with Unauthenticated.
	err = verifier.Verify(t.Context(), "alice@example.com", enr.Secret, code)
	if err == nil {
		t.Fatal("expected replay attempt rejected")
	}
	f, ok := errors.AsType[*fault.Error](err)
	if !ok || f.Code() != fault.CodeUnauthenticated {
		t.Fatalf("expected fault.CodeUnauthenticated, got %v", err)
	}

	// Invalid code returns Unauthenticated.
	if err := verifier.Verify(t.Context(), "alice@example.com", enr.Secret, "000000"); code != "000000" && err == nil {
		t.Fatal("expected invalid code rejected")
	}

	// Advancing to the next TOTP window accepts the new step's code.
	now = now.Add(30 * time.Second)
	nextCode, err := totp.GenerateCode(enr.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(t.Context(), "alice@example.com", enr.Secret, nextCode); err != nil {
		t.Fatalf("next window Verify failed: %v", err)
	}
}

func TestHOTPGenerateAndVerify(t *testing.T) {
	t.Parallel()

	verifier, err := mfa.NewHOTP(mfa.HOTPConfig{Issuer: "KitApp"})
	if err != nil {
		t.Fatalf("NewHOTP failed: %v", err)
	}

	enr, err := verifier.Generate("bob@example.com")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !strings.HasPrefix(enr.URI, "otpauth://hotp/") {
		t.Fatalf("unexpected URI: %q", enr.URI)
	}

	code, err := hotp.GenerateCode(enr.Secret, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(enr.Secret, code, 7); err != nil {
		t.Fatalf("HOTP Verify failed: %v", err)
	}
	if err := verifier.Verify(enr.Secret, code, 8); err == nil {
		t.Fatal("expected wrong HOTP counter rejected")
	}
}

func TestMemoryReplayStoreCapacityExhaustionAndExpiryRecovery(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0).UTC()
	store := mfa.NewMemoryReplayStore(2, func() time.Time { return now })

	if ok, err := store.Use(t.Context(), "k1", 30*time.Second); err != nil || !ok {
		t.Fatalf("k1 first use = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := store.Use(t.Context(), "k2", 60*time.Second); err != nil || !ok {
		t.Fatalf("k2 first use = (%v, %v), want (true, nil)", ok, err)
	}

	// Capacity full of live entries: reject new keys without evicting live ones.
	if ok, err := store.Use(t.Context(), "k3", 30*time.Second); !errors.Is(err, mfa.ErrReplayStoreFull) || ok {
		t.Fatalf("k3 while full = (%v, %v), want (false, ErrReplayStoreFull)", ok, err)
	}
	if ok, err := store.Use(t.Context(), "k1", 30*time.Second); err != nil || ok {
		t.Fatalf("live k1 must still be rejected as replay: (%v, %v)", ok, err)
	}
	if ok, err := store.Use(t.Context(), "k2", 60*time.Second); err != nil || ok {
		t.Fatalf("live k2 must still be rejected as replay: (%v, %v)", ok, err)
	}

	// TOTP.Verify propagates ErrReplayStoreFull cleanly.
	verifier, err := mfa.NewTOTP(mfa.TOTPConfig{
		Issuer: "KitApp",
		Clock:  func() time.Time { return now },
		Period: 30,
	}, mfa.WithReplayStore(store))
	if err != nil {
		t.Fatal(err)
	}
	enr, err := verifier.Generate("carol@example.com")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(enr.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(t.Context(), "carol@example.com", enr.Secret, code); !errors.Is(err, mfa.ErrReplayStoreFull) {
		t.Fatalf("Verify while full = %v, want ErrReplayStoreFull", err)
	}

	// Advance clock past k1 expiry (30s): k1 clears, k3 admitted, k2 still unexpired until 60s.
	now = now.Add(30 * time.Second)
	if ok, err := store.Use(t.Context(), "k2", 60*time.Second); err != nil || ok {
		t.Fatalf("unexpired k2 evicted prematurely: (%v, %v)", ok, err)
	}
	if ok, err := store.Use(t.Context(), "k3", 30*time.Second); err != nil || !ok {
		t.Fatalf("k3 after k1 expiry = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestConfigValidationRejectsUnsafeAlgorithmAndDigits(t *testing.T) {
	t.Parallel()

	if _, err := mfa.NewTOTP(mfa.TOTPConfig{Issuer: "App", Digits: 99}); err == nil {
		t.Fatal("expected error for invalid TOTP digits")
	}
	if _, err := mfa.NewTOTP(mfa.TOTPConfig{Issuer: "App", Algorithm: 99}); err == nil {
		t.Fatal("expected error for invalid TOTP algorithm")
	}
	if _, err := mfa.NewHOTP(mfa.HOTPConfig{Issuer: "App", Digits: -1}); err == nil {
		t.Fatal("expected error for negative HOTP digits")
	}
	negStore := mfa.NewMemoryReplayStore(-5)
	if ok, err := negStore.Use(t.Context(), "k1", time.Second); err != nil || !ok {
		t.Fatalf("negative maxEntries store Use = (%v, %v), want (true, nil)", ok, err)
	}
}
