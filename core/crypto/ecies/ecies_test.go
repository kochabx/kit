package ecies

import (
	"bytes"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	suite := X25519ChaCha20()
	privateKey, err := suite.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	info, aad, want := []byte("example/orders/v1"), []byte("order:42"), []byte("classified")
	message, err := suite.Seal(privateKey.Public(), info, aad, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := suite.Open(privateKey, info, aad, message)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Open() = %q, want %q", got, want)
	}
}

func TestAuthentication(t *testing.T) {
	suite := X25519ChaCha20()
	key, err := suite.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	message, err := suite.Seal(key.Public(), []byte("protocol"), []byte("record:1"), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		info, aad []byte
		tamper    bool
	}{
		{"wrong info", []byte("other"), []byte("record:1"), false},
		{"wrong aad", []byte("protocol"), []byte("record:2"), false},
		{"tampered ciphertext", []byte("protocol"), []byte("record:1"), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := &Message{bytes.Clone(message.Encapsulation), bytes.Clone(message.Ciphertext)}
			if test.tamper {
				m.Ciphertext[0] ^= 1
			}
			_, err := suite.Open(key, test.info, test.aad, m)
			if !errors.Is(err, ErrDecryptionFailed) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestKeySerializationAndEmptyPlaintext(t *testing.T) {
	suite := X25519ChaCha20()
	key, err := suite.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateBytes, err := key.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := suite.ParsePrivateKey(privateBytes)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := suite.ParsePublicKey(key.Public().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	message, err := suite.Seal(publicKey, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := suite.Open(privateKey, nil, nil, message)
	if err != nil {
		t.Fatal(err)
	}
	if len(plaintext) != 0 {
		t.Fatalf("plaintext = %#v", plaintext)
	}
}

func TestHybridPostQuantumRoundTrip(t *testing.T) {
	suite := HybridPostQuantum()
	key, err := suite.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	message, err := suite.Seal(key.Public(), []byte("pq-test"), nil, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := suite.Open(key, []byte("pq-test"), nil, message)
	if err != nil || string(plaintext) != "secret" {
		t.Fatalf("Open() = %q, %v", plaintext, err)
	}
}
