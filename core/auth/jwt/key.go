package jwt

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"fmt"
	"sync"

	gojwt "github.com/golang-jwt/jwt/v5"
)

type SigningKey struct {
	ID     string
	Method gojwt.SigningMethod
	Key    any
}
type VerificationKey struct {
	Method gojwt.SigningMethod
	Key    any
}

type KeyPair struct {
	ID        string
	Method    gojwt.SigningMethod
	SignKey   any
	VerifyKey any
}

type KeyProvider interface {
	SigningKey(context.Context) (SigningKey, error)
	VerificationKey(context.Context, string, string) (VerificationKey, error)
}

type HMACKeyProvider struct {
	id     string
	method *gojwt.SigningMethodHMAC
	secret []byte
}

// KeySet supports zero-downtime rotation: Sign always uses current while
// VerificationKey continues to serve retained historical keys by kid.
type KeySet struct {
	mu      sync.RWMutex
	current KeyPair
	verify  map[string]VerificationKey
}

func NewKeySet(current KeyPair, historical ...KeyPair) (*KeySet, error) {
	set := &KeySet{verify: make(map[string]VerificationKey)}
	if err := set.rotate(current); err != nil {
		return nil, err
	}
	for _, key := range historical {
		if err := validateKeyPair(key); err != nil {
			return nil, err
		}
		if _, exists := set.verify[key.ID]; exists {
			return nil, ErrConfigInvalid
		}
		set.verify[key.ID] = VerificationKey{key.Method, key.VerifyKey}
	}
	return set, nil
}
func validateKeyPair(key KeyPair) error {
	if key.ID == "" || key.Method == nil || key.SignKey == nil || key.VerifyKey == nil {
		return ErrConfigInvalid
	}
	switch key.Method.(type) {
	case *gojwt.SigningMethodHMAC:
		sign, ok := key.SignKey.([]byte)
		if !ok || len(sign) < 32 {
			return ErrConfigInvalid
		}
		verify, ok := key.VerifyKey.([]byte)
		if !ok || len(verify) < 32 || !bytes.Equal(sign, verify) {
			return ErrConfigInvalid
		}
	case *gojwt.SigningMethodRSA, *gojwt.SigningMethodRSAPSS:
		private, ok := key.SignKey.(*rsa.PrivateKey)
		if !ok {
			return ErrConfigInvalid
		}
		public, ok := key.VerifyKey.(*rsa.PublicKey)
		if !ok || private.PublicKey.N.Cmp(public.N) != 0 || private.PublicKey.E != public.E {
			return ErrConfigInvalid
		}
	case *gojwt.SigningMethodECDSA:
		private, ok := key.SignKey.(*ecdsa.PrivateKey)
		if !ok {
			return ErrConfigInvalid
		}
		public, ok := key.VerifyKey.(*ecdsa.PublicKey)
		if !ok ||
			private.PublicKey.Curve != public.Curve ||
			private.PublicKey.X.Cmp(public.X) != 0 ||
			private.PublicKey.Y.Cmp(public.Y) != 0 {
			return ErrConfigInvalid
		}
	default:
		return ErrConfigInvalid
	}
	return nil
}
func (s *KeySet) rotate(key KeyPair) error {
	if err := validateKeyPair(key); err != nil {
		return err
	}
	s.current = key
	s.verify[key.ID] = VerificationKey{key.Method, key.VerifyKey}
	return nil
}
func (s *KeySet) Rotate(key KeyPair) error { s.mu.Lock(); defer s.mu.Unlock(); return s.rotate(key) }
func (s *KeySet) SigningKey(context.Context) (SigningKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SigningKey{s.current.ID, s.current.Method, s.current.SignKey}, nil
}
func (s *KeySet) VerificationKey(_ context.Context, kid, alg string) (VerificationKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.verify[kid]
	if !ok || key.Method.Alg() != alg {
		return VerificationKey{}, ErrInvalidSignature
	}
	return key, nil
}

func NewHMACKeyProvider(id string, method *gojwt.SigningMethodHMAC, secret []byte) (*HMACKeyProvider, error) {
	if id == "" || method == nil || len(secret) < 32 {
		return nil, fmt.Errorf("%w: HMAC requires kid and at least 32 bytes", ErrConfigInvalid)
	}
	return &HMACKeyProvider{id: id, method: method, secret: append([]byte(nil), secret...)}, nil
}
func (p *HMACKeyProvider) SigningKey(context.Context) (SigningKey, error) {
	return SigningKey{p.id, p.method, p.secret}, nil
}
func (p *HMACKeyProvider) VerificationKey(_ context.Context, kid, alg string) (VerificationKey, error) {
	if kid != p.id || alg != p.method.Alg() {
		return VerificationKey{}, ErrInvalidSignature
	}
	return VerificationKey{p.method, p.secret}, nil
}
