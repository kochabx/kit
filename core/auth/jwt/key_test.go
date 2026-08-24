package jwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	gojwt "github.com/golang-jwt/jwt/v5"
	kitjwt "github.com/kochabx/kit/core/auth/jwt"
	"github.com/stretchr/testify/require"
)

func TestKeySetSeparatesSigningAndVerificationKeys(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	set, err := kitjwt.NewKeySet(kitjwt.KeyPair{ID: "rsa-1", Method: gojwt.SigningMethodRS256, SignKey: private, VerifyKey: &private.PublicKey})
	require.NoError(t, err)
	signing, err := set.SigningKey(context.Background())
	require.NoError(t, err)
	require.Same(t, private, signing.Key)
	verification, err := set.VerificationKey(context.Background(), "rsa-1", "RS256")
	require.NoError(t, err)
	require.Same(t, &private.PublicKey, verification.Key)
	next, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	require.NoError(t, set.Rotate(kitjwt.KeyPair{ID: "ec-1", Method: gojwt.SigningMethodES256, SignKey: next, VerifyKey: &next.PublicKey}))
	_, err = set.VerificationKey(context.Background(), "rsa-1", "RS256")
	require.NoError(t, err)
}

func TestKeySetRejectsMismatchedPair(t *testing.T) {
	first, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	second, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	_, err = kitjwt.NewKeySet(kitjwt.KeyPair{ID: "bad", Method: gojwt.SigningMethodRS256, SignKey: first, VerifyKey: &second.PublicKey})
	require.ErrorIs(t, err, kitjwt.ErrConfigInvalid)
}

func TestKeySetRejectsMismatchedHMACSecrets(t *testing.T) {
	_, err := kitjwt.NewKeySet(kitjwt.KeyPair{
		ID:        "hmac-1",
		Method:    gojwt.SigningMethodHS256,
		SignKey:   []byte("0123456789abcdef0123456789abcdef"),
		VerifyKey: []byte("abcdef0123456789abcdef0123456789"),
	})
	require.ErrorIs(t, err, kitjwt.ErrConfigInvalid)
}

func TestKeySetRejectsDuplicateKeyIDs(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	key := kitjwt.KeyPair{
		ID:        "duplicate",
		Method:    gojwt.SigningMethodHS256,
		SignKey:   secret,
		VerifyKey: secret,
	}
	_, err := kitjwt.NewKeySet(key, key)
	require.ErrorIs(t, err, kitjwt.ErrConfigInvalid)
}
