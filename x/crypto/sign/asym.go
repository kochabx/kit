package sign

import (
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"fmt"
)

// GenerateEd25519 generates an Ed25519 key pair backed by crypto/rand.
func GenerateEd25519() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// SignEd25519 computes an Ed25519 signature over message.
func SignEd25519(priv ed25519.PrivateKey, message []byte) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: invalid Ed25519 private key length", ErrInvalidKey)
	}
	return ed25519.Sign(priv, message), nil
}

// VerifyEd25519 verifies an Ed25519 signature over message.
func VerifyEd25519(pub ed25519.PublicKey, message, signature []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: invalid Ed25519 public key length", ErrInvalidKey)
	}
	if !ed25519.Verify(pub, message, signature) {
		return ErrSignatureMismatch
	}
	return nil
}

// MLDSAParameters identifies a NIST FIPS 204 ML-DSA parameter set.
type MLDSAParameters = mldsa.Parameters

// MLDSA44 returns NIST FIPS 204 ML-DSA-44 parameters.
func MLDSA44() MLDSAParameters { return mldsa.MLDSA44() }

// MLDSA65 returns NIST FIPS 204 ML-DSA-65 parameters.
func MLDSA65() MLDSAParameters { return mldsa.MLDSA65() }

// MLDSA87 returns NIST FIPS 204 ML-DSA-87 parameters.
func MLDSA87() MLDSAParameters { return mldsa.MLDSA87() }

// GenerateMLDSA generates a FIPS 204 ML-DSA key pair for params (e.g. MLDSA65()).
func GenerateMLDSA(params MLDSAParameters) (*mldsa.PrivateKey, error) {
	priv, err := mldsa.GenerateKey(params)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return priv, nil
}

// ParseMLDSAPrivateKey reconstructs an ML-DSA private key from its 32-byte seed.
func ParseMLDSAPrivateKey(params MLDSAParameters, seed []byte) (*mldsa.PrivateKey, error) {
	priv, err := mldsa.NewPrivateKey(params, seed)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return priv, nil
}

// ParseMLDSAPublicKey reconstructs an ML-DSA public key from encoded bytes.
func ParseMLDSAPublicKey(params MLDSAParameters, raw []byte) (*mldsa.PublicKey, error) {
	pub, err := mldsa.NewPublicKey(params, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return pub, nil
}

// SignMLDSA signs message with optional domain-separation context string (<= 255 bytes).
func SignMLDSA(priv *mldsa.PrivateKey, message []byte, contextStr string) ([]byte, error) {
	if priv == nil {
		return nil, fmt.Errorf("%w: nil ML-DSA private key", ErrInvalidKey)
	}
	var opts *mldsa.Options
	if contextStr != "" {
		opts = &mldsa.Options{Context: contextStr}
	}
	return priv.Sign(rand.Reader, message, opts)
}

// VerifyMLDSA verifies an ML-DSA signature under pub and contextStr.
func VerifyMLDSA(pub *mldsa.PublicKey, message, signature []byte, contextStr string) error {
	if pub == nil {
		return fmt.Errorf("%w: nil ML-DSA public key", ErrInvalidKey)
	}
	var opts *mldsa.Options
	if contextStr != "" {
		opts = &mldsa.Options{Context: contextStr}
	}
	if err := mldsa.Verify(pub, message, signature, opts); err != nil {
		return fmt.Errorf("%w: %w", ErrSignatureMismatch, err)
	}
	return nil
}
