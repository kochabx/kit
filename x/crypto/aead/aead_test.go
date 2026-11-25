package aead

import (
	"bytes"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
)

func randomKey(t testing.TB) []byte {
	t.Helper()
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptAADAndHKDFUniqueness(t *testing.T) {
	k1 := randomKey(t)
	c, err := New(k1, WithKeyID("k1"), WithMaxPlaintext(1024))
	if err != nil {
		t.Fatal(err)
	}

	plain := []byte("db-password=super-secret")
	aad := []byte("user:42")
	ct1, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := c.Encrypt(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Fatal("HKDF salt/nonce must produce distinct envelopes for identical plaintext")
	}

	got, err := c.Decrypt(ct1, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("Decrypt = (%q, %v)", got, err)
	}
	if _, err := c.Decrypt(ct1, []byte("wrong-aad")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("expected ErrDecryptionFailed for wrong AAD, got %v", err)
	}
	// Mutating header kid byte must fail authentication because header is bound to AAD.
	tampered := slicesCloneAndTamper(ct1, 6)
	if _, err := c.Decrypt(tampered, aad); err == nil {
		t.Fatal("expected tampered header to fail decryption")
	}

	if _, err := c.Encrypt(make([]byte, 2048), nil); !errors.Is(err, ErrPlaintextTooLarge) {
		t.Fatalf("expected ErrPlaintextTooLarge, got %v", err)
	}
}

func slicesCloneAndTamper(b []byte, idx int) []byte {
	cp := append([]byte(nil), b...)
	cp[idx] ^= 0xff
	return cp
}

func TestRotationNeedsReencryptAndConcurrency(t *testing.T) {
	oldKey := randomKey(t)
	newKey := randomKey(t)

	cOld, err := New(oldKey, WithKeyID("v1"))
	if err != nil {
		t.Fatal(err)
	}
	sealedStr, err := cOld.EncryptString([]byte("token"), []byte("scope"))
	if err != nil {
		t.Fatal(err)
	}

	cNew, err := New(newKey, WithKeyID("v2"), WithDecryptionKey("v1", oldKey))
	if err != nil {
		t.Fatal(err)
	}
	rawOld, err := cOld.Encrypt([]byte("token"), []byte("scope"))
	if err != nil {
		t.Fatal(err)
	}
	if kid, err := cNew.KeyID(rawOld); err != nil || kid != "v1" {
		t.Fatalf("KeyID = (%q, %v), want v1", kid, err)
	}
	if need, err := cNew.NeedsReencrypt(rawOld); err != nil || !need {
		t.Fatalf("NeedsReencrypt(rawOld) = (%v, %v), want true", need, err)
	}

	pt, err := cNew.DecryptString(sealedStr, []byte("scope"))
	if err != nil || string(pt) != "token" {
		t.Fatalf("DecryptString = (%q, %v)", pt, err)
	}

	reencrypted, err := cNew.Reencrypt(rawOld, []byte("scope"))
	if err != nil {
		t.Fatal(err)
	}
	if need, err := cNew.NeedsReencrypt(reencrypted); err != nil || need {
		t.Fatalf("NeedsReencrypt(reencrypted) = (%v, %v), want false", need, err)
	}
	if pt2, err := cNew.Decrypt(reencrypted, []byte("scope")); err != nil || string(pt2) != "token" {
		t.Fatalf("Decrypt reencrypted = (%q, %v)", pt2, err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				ct, err := cNew.Encrypt([]byte("concurrent"), nil)
				if err != nil {
					t.Error(err)
					return
				}
				if got, err := cNew.Decrypt(ct, nil); err != nil || string(got) != "concurrent" {
					t.Errorf("decrypt mismatch: %q %v", got, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
