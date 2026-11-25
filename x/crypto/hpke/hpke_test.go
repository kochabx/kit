package hpke

import (
	"bytes"
	"errors"
	"testing"
)

func TestClassicAndHybridPQSealOpen(t *testing.T) {
	for _, tc := range []struct {
		name  string
		suite Suite
	}{
		{"classic_x25519", SuiteClassicX25519()},
		{"hybrid_pq_mlkem768_x25519_shake256", SuiteHybridPQ()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			priv, err := GenerateKeyPair(tc.suite, "k-2026-01")
			if err != nil {
				t.Fatalf("GenerateKeyPair failed: %v", err)
			}
			privRaw, err := priv.Bytes()
			if err != nil {
				t.Fatalf("priv.Bytes() failed: %v", err)
			}
			parsedPriv, err := ParsePrivateKey(tc.suite, "k-2026-01", privRaw)
			if err != nil {
				t.Fatalf("ParsePrivateKey failed: %v", err)
			}
			parsedPub, err := ParsePublicKey(tc.suite, "k-2026-01", priv.PublicKey().Bytes())
			if err != nil {
				t.Fatalf("ParsePublicKey failed: %v", err)
			}

			msg := []byte("sensitive credential payload")
			info := []byte("tenant:acme")
			env, err := Seal(parsedPub, msg, info)
			if err != nil {
				t.Fatalf("Seal failed: %v", err)
			}
			if kid, err := KeyID(env); err != nil || kid != "k-2026-01" {
				t.Fatalf("KeyID = (%q, %v), want k-2026-01", kid, err)
			}

			got, err := Open(parsedPriv, env, info)
			if err != nil || !bytes.Equal(got, msg) {
				t.Fatalf("Open = (%q, %v), want %q", got, err, msg)
			}
			if _, err := Open(parsedPriv, env, []byte("wrong-info")); !errors.Is(err, ErrDecryptionFailed) {
				t.Fatalf("expected ErrDecryptionFailed on info mismatch, got %v", err)
			}
		})
	}
}

func TestKeyringRotation(t *testing.T) {
	k1, err := GenerateKeyPair(SuiteClassicX25519(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := GenerateKeyPair(SuiteHybridPQ(), "v2")
	if err != nil {
		t.Fatal(err)
	}

	kr, err := NewKeyring(k1)
	if err != nil {
		t.Fatal(err)
	}
	env1, err := kr.Seal([]byte("secret-v1"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}

	if err := kr.AddPrivateKey(k2); err != nil {
		t.Fatal(err)
	}
	if err := kr.SetPrimary("v2"); err != nil {
		t.Fatal(err)
	}
	if need, err := kr.NeedsReencrypt(env1); err != nil || !need {
		t.Fatalf("NeedsReencrypt(env1) = (%v, %v), want true", need, err)
	}
	pt1, err := kr.Open(env1, []byte("aad"))
	if err != nil || string(pt1) != "secret-v1" {
		t.Fatalf("Open old key = (%q, %v)", pt1, err)
	}

	env2, err := kr.Seal([]byte("secret-v2"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if need, err := kr.NeedsReencrypt(env2); err != nil || need {
		t.Fatalf("NeedsReencrypt(env2) = (%v, %v), want false", need, err)
	}
	pt2, err := kr.Open(env2, []byte("aad"))
	if err != nil || string(pt2) != "secret-v2" {
		t.Fatalf("Open new key = (%q, %v)", pt2, err)
	}
}
