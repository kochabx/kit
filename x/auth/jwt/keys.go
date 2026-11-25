package jwt

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

type ringEntry struct {
	kid       string
	alg       jwa.SignatureAlgorithm
	signKey   jwk.Key
	verifyKey jwk.Key
	symmetric bool
}

// KeyRing manages signing keys and verification keys with zero-downtime key rotation.
type KeyRing struct {
	mu      sync.RWMutex
	active  string
	entries map[string]ringEntry
	order   []string
}

// NewKeyRing creates an empty KeyRing.
func NewKeyRing() *KeyRing {
	return &KeyRing{
		entries: make(map[string]ringEntry),
	}
}

// Add registers a raw signing key (*rsa.PrivateKey, ed25519.PrivateKey, []byte, or jwk.Key) under kid.
// The first key added automatically becomes the active signing key.
func (r *KeyRing) Add(kid string, alg jwa.SignatureAlgorithm, rawKey any) error {
	if kid == "" {
		return errors.New("jwt: kid is required")
	}
	if alg.String() == "" || alg == jwa.NoSignature() {
		return fmt.Errorf("jwt: invalid signature algorithm %q", alg)
	}
	if rawKey == nil {
		return errors.New("jwt: signing key is required")
	}

	var (
		signKey jwk.Key
		err     error
	)
	if k, ok := rawKey.(jwk.Key); ok {
		signKey, err = k.Clone()
		if err != nil {
			return fmt.Errorf("jwt: clone key %q: %w", kid, err)
		}
	} else {
		signKey, err = jwk.Import(rawKey)
		if err != nil {
			return fmt.Errorf("jwt: import key %q: %w", kid, err)
		}
	}
	if err := signKey.Set(jwk.KeyIDKey, kid); err != nil {
		return fmt.Errorf("jwt: set kid on key %q: %w", kid, err)
	}
	if err := signKey.Set(jwk.AlgorithmKey, alg); err != nil {
		return fmt.Errorf("jwt: set alg on key %q: %w", kid, err)
	}

	_, rawSym := rawKey.([]byte)
	symmetric := rawSym || signKey.KeyType() == jwa.OctetSeq()
	canSign := symmetric
	var verifyKey jwk.Key
	if symmetric {
		verifyKey = signKey
	} else {
		var exported any
		if expErr := jwk.Export(signKey, &exported); expErr == nil {
			_, canSign = exported.(crypto.Signer)
		}
		verifyKey, err = jwk.PublicKeyOf(signKey)
		if err != nil {
			// Fallback to signKey itself if rawKey was already public/verify-only.
			verifyKey = signKey
		} else {
			_ = verifyKey.Set(jwk.KeyIDKey, kid)
			_ = verifyKey.Set(jwk.AlgorithmKey, alg)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if kid == r.active && !canSign {
		return fmt.Errorf("jwt: cannot replace active signing key %q with verify-only key", kid)
	}
	if _, exists := r.entries[kid]; !exists {
		r.order = append(r.order, kid)
	}
	entry := ringEntry{
		kid:       kid,
		alg:       alg,
		verifyKey: verifyKey,
		symmetric: symmetric,
	}
	if canSign {
		entry.signKey = signKey
	}
	r.entries[kid] = entry
	if r.active == "" && canSign {
		r.active = kid
	}
	return nil
}

// AddEd25519 adds an Ed25519 private signing key under kid.
func (r *KeyRing) AddEd25519(kid string, priv ed25519.PrivateKey) error {
	if len(priv) != ed25519.PrivateKeySize {
		return errors.New("jwt: invalid ed25519 private key length")
	}
	return r.Add(kid, jwa.EdDSA(), priv)
}

// AddHMAC adds a symmetric HMAC secret under kid (defaults to HS256).
func (r *KeyRing) AddHMAC(kid string, secret []byte, alg ...jwa.SignatureAlgorithm) error {
	if len(secret) < 32 {
		return errors.New("jwt: HMAC secret must be at least 32 bytes")
	}
	selected := jwa.HS256()
	if len(alg) > 0 && alg[0].String() != "" {
		selected = alg[0]
	}
	return r.Add(kid, selected, slices.Clone(secret))
}

// AddRSA adds an RSA private signing key under kid (defaults to RS256).
func (r *KeyRing) AddRSA(kid string, priv *rsa.PrivateKey, alg ...jwa.SignatureAlgorithm) error {
	if priv == nil {
		return errors.New("jwt: rsa private key is required")
	}
	selected := jwa.RS256()
	if len(alg) > 0 && alg[0].String() != "" {
		selected = alg[0]
	}
	return r.Add(kid, selected, priv)
}

// SetActive switches the active signing key to a previously added kid.
func (r *KeyRing) SetActive(kid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[kid]
	if !ok {
		return fmt.Errorf("jwt: kid %q not found in keyring", kid)
	}
	if entry.signKey == nil {
		return fmt.Errorf("jwt: kid %q cannot be used for signing", kid)
	}
	r.active = kid
	return nil
}

// Remove deletes a retired key from the ring. Rejects removing the active signing key.
func (r *KeyRing) Remove(kid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kid == r.active {
		return errors.New("jwt: cannot remove active signing key")
	}
	delete(r.entries, kid)
	r.order = slices.DeleteFunc(r.order, func(id string) bool { return id == kid })
	return nil
}

func (r *KeyRing) signingKey() (jwk.Key, jwa.SignatureAlgorithm, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[r.active]
	if !ok || entry.signKey == nil {
		return nil, jwa.SignatureAlgorithm{}, errors.New("jwt: no active signing key")
	}
	return entry.signKey, entry.alg, nil
}

func (r *KeyRing) verifySet() (jwk.Set, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.entries) == 0 {
		return nil, errors.New("jwt: keyring is empty")
	}
	set := jwk.NewSet()
	for _, kid := range r.order {
		if err := set.AddKey(r.entries[kid].verifyKey); err != nil {
			return nil, fmt.Errorf("jwt: add verify key %q: %w", kid, err)
		}
	}
	return set, nil
}

// PublicJWKS returns a JWK Set containing only asymmetric public verification keys.
func (r *KeyRing) PublicJWKS() (jwk.Set, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := jwk.NewSet()
	for _, kid := range r.order {
		entry := r.entries[kid]
		if entry.symmetric {
			continue
		}
		if err := set.AddKey(entry.verifyKey); err != nil {
			return nil, fmt.Errorf("jwt: export public jwk %q: %w", kid, err)
		}
	}
	return set, nil
}
