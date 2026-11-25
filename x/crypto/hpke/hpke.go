// Package hpke wraps crypto/hpke (RFC 9180) with suite/kid metadata
// and seamless zero-downtime key rotation via Keyring.
package hpke

import (
	"crypto/ecdh"
	stdhpke "crypto/hpke"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
)

var (
	magic = [4]byte{'K', 'H', 'P', 'K'}

	ErrInvalidSuite     = errors.New("invalid HPKE suite")
	ErrInvalidKey       = errors.New("invalid HPKE key")
	ErrUnknownKey       = errors.New("unknown HPKE key ID")
	ErrInvalidMessage   = errors.New("invalid HPKE envelope")
	ErrEncryptionFailed = errors.New("HPKE encryption failed")
	ErrDecryptionFailed = errors.New("HPKE decryption failed")
)

const (
	envelopeVersion uint8 = 1
	maxKeyIDLen           = 255
	suiteIDClassic  uint8 = 1
	suiteIDHybridPQ uint8 = 2
)

// Suite selects an immutable RFC 9180 KEM/KDF/AEAD combination.
type Suite struct {
	ID   uint8
	Name string
	kem  stdhpke.KEM
	kdf  stdhpke.KDF
	aead stdhpke.AEAD
}

// SuiteClassicX25519 returns DHKEM(X25519) + HKDF-SHA256 + AES-256-GCM.
func SuiteClassicX25519() Suite {
	return Suite{
		ID:   suiteIDClassic,
		Name: "X25519-HKDF-SHA256-AES256GCM",
		kem:  stdhpke.DHKEM(ecdh.X25519()),
		kdf:  stdhpke.HKDFSHA256(),
		aead: stdhpke.AES256GCM(),
	}
}

// SuiteHybridPQ returns ML-KEM-768+X25519 + SHAKE256 + AES-256-GCM.
func SuiteHybridPQ() Suite {
	return Suite{
		ID:   suiteIDHybridPQ,
		Name: "MLKEM768X25519-SHAKE256-AES256GCM",
		kem:  stdhpke.MLKEM768X25519(),
		kdf:  stdhpke.SHAKE256(),
		aead: stdhpke.AES256GCM(),
	}
}

func (s Suite) valid() bool {
	return s.ID != 0 && s.kem != nil && s.kdf != nil && s.aead != nil
}

// PublicKey binds a key ID and Suite to an RFC 9180 HPKE public key.
type PublicKey struct {
	suite Suite
	kid   string
	key   stdhpke.PublicKey
}

// PrivateKey binds a key ID and Suite to an RFC 9180 HPKE private key.
type PrivateKey struct {
	suite Suite
	kid   string
	key   stdhpke.PrivateKey
}

func (pk *PublicKey) KeyID() string  { return pk.kid }
func (pk *PublicKey) Suite() Suite   { return pk.suite }
func (pk *PublicKey) Bytes() []byte  { return slices.Clone(pk.key.Bytes()) }
func (sk *PrivateKey) KeyID() string { return sk.kid }
func (sk *PrivateKey) Suite() Suite  { return sk.suite }

func (sk *PrivateKey) Bytes() ([]byte, error) {
	return sk.key.Bytes()
}

func (sk *PrivateKey) PublicKey() *PublicKey {
	return &PublicKey{suite: sk.suite, kid: sk.kid, key: sk.key.PublicKey()}
}

// GenerateKeyPair creates a fresh HPKE private/public key pair for suite and kid.
func GenerateKeyPair(suite Suite, kid string) (*PrivateKey, error) {
	if !suite.valid() {
		return nil, ErrInvalidSuite
	}
	if kid == "" || len(kid) > maxKeyIDLen {
		return nil, fmt.Errorf("%w: invalid key ID", ErrInvalidKey)
	}
	priv, err := suite.kem.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("%w: generate: %w", ErrInvalidKey, err)
	}
	return &PrivateKey{suite: suite, kid: kid, key: priv}, nil
}

// ParsePublicKey deserializes an RFC 9180 public key encoded via Bytes().
func ParsePublicKey(suite Suite, kid string, raw []byte) (*PublicKey, error) {
	if !suite.valid() {
		return nil, ErrInvalidSuite
	}
	if kid == "" || len(kid) > maxKeyIDLen || len(raw) == 0 {
		return nil, ErrInvalidKey
	}
	pub, err := suite.kem.NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return &PublicKey{suite: suite, kid: kid, key: pub}, nil
}

// ParsePrivateKey deserializes an RFC 9180 private key encoded via Bytes().
func ParsePrivateKey(suite Suite, kid string, raw []byte) (*PrivateKey, error) {
	if !suite.valid() {
		return nil, ErrInvalidSuite
	}
	if kid == "" || len(kid) > maxKeyIDLen || len(raw) == 0 {
		return nil, ErrInvalidKey
	}
	priv, err := suite.kem.NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	return &PrivateKey{suite: suite, kid: kid, key: priv}, nil
}

// Seal encrypts plaintext under pub while authenticating suite, kid, and caller info.
func Seal(pub *PublicKey, plaintext, info []byte) ([]byte, error) {
	if pub == nil || pub.key == nil || !pub.suite.valid() {
		return nil, ErrInvalidKey
	}
	hdr := buildHeader(pub.suite.ID, pub.kid)
	boundInfo := make([]byte, len(hdr)+len(info))
	copy(boundInfo, hdr)
	copy(boundInfo[len(hdr):], info)

	ct, err := stdhpke.Seal(pub.key, pub.suite.kdf, pub.suite.aead, boundInfo, plaintext)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEncryptionFailed, err)
	}
	out := make([]byte, len(hdr)+len(ct))
	copy(out, hdr)
	copy(out[len(hdr):], ct)
	return out, nil
}

// Open decrypts an envelope produced by Seal using priv.
func Open(priv *PrivateKey, envelope, info []byte) ([]byte, error) {
	if priv == nil || priv.key == nil || !priv.suite.valid() {
		return nil, ErrInvalidKey
	}
	hdr, suiteID, kid, ct, err := parseHeader(envelope)
	if err != nil {
		return nil, err
	}
	if suiteID != priv.suite.ID {
		return nil, fmt.Errorf("%w: envelope suite %d != key suite %d", ErrInvalidSuite, suiteID, priv.suite.ID)
	}
	if kid != priv.kid {
		return nil, fmt.Errorf("%w: envelope key %q != key %q", ErrUnknownKey, kid, priv.kid)
	}
	boundInfo := make([]byte, len(hdr)+len(info))
	copy(boundInfo, hdr)
	copy(boundInfo[len(hdr):], info)

	pt, err := stdhpke.Open(priv.key, priv.suite.kdf, priv.suite.aead, boundInfo, ct)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecryptionFailed, err)
	}
	return pt, nil
}

// KeyID extracts the public key identifier from an envelope without decrypting.
func KeyID(envelope []byte) (string, error) {
	_, _, kid, _, err := parseHeader(envelope)
	return kid, err
}

// Keyring manages concurrent encryption with a primary key and decryption with any registered key.
type Keyring struct {
	mu      sync.RWMutex
	primary string
	privs   map[string]*PrivateKey
	pubs    map[string]*PublicKey
}

// NewKeyring constructs a Keyring with primary set as the initial sealing and opening key.
func NewKeyring(primary *PrivateKey) (*Keyring, error) {
	if primary == nil || primary.key == nil {
		return nil, ErrInvalidKey
	}
	kr := &Keyring{
		primary: primary.kid,
		privs:   make(map[string]*PrivateKey),
		pubs:    make(map[string]*PublicKey),
	}
	kr.privs[primary.kid] = primary
	kr.pubs[primary.kid] = primary.PublicKey()
	return kr, nil
}

func (kr *Keyring) AddPrivateKey(priv *PrivateKey) error {
	if priv == nil || priv.key == nil {
		return ErrInvalidKey
	}
	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.privs[priv.kid] = priv
	kr.pubs[priv.kid] = priv.PublicKey()
	return nil
}

func (kr *Keyring) AddPublicKey(pub *PublicKey) error {
	if pub == nil || pub.key == nil {
		return ErrInvalidKey
	}
	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.pubs[pub.kid] = pub
	return nil
}

func (kr *Keyring) SetPrimary(kid string) error {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	if _, ok := kr.pubs[kid]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, kid)
	}
	kr.primary = kid
	return nil
}

func (kr *Keyring) Seal(plaintext, info []byte) ([]byte, error) {
	kr.mu.RLock()
	pub := kr.pubs[kr.primary]
	kr.mu.RUnlock()
	return Seal(pub, plaintext, info)
}

func (kr *Keyring) Open(envelope, info []byte) ([]byte, error) {
	_, _, kid, _, err := parseHeader(envelope)
	if err != nil {
		return nil, err
	}
	kr.mu.RLock()
	priv, ok := kr.privs[kid]
	kr.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKey, kid)
	}
	return Open(priv, envelope, info)
}

func (kr *Keyring) NeedsReencrypt(envelope []byte) (bool, error) {
	kid, err := KeyID(envelope)
	if err != nil {
		return false, err
	}
	kr.mu.RLock()
	primary := kr.primary
	kr.mu.RUnlock()
	return kid != primary, nil
}

func (kr *Keyring) KeyIDs() []string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return slices.Collect(maps.Keys(kr.pubs))
}

func buildHeader(suiteID uint8, kid string) []byte {
	hdr := make([]byte, 4+1+1+1+len(kid))
	copy(hdr[:4], magic[:])
	hdr[4] = envelopeVersion
	hdr[5] = suiteID
	hdr[6] = byte(len(kid))
	copy(hdr[7:], kid)
	return hdr
}

func parseHeader(envelope []byte) (hdr []byte, suiteID uint8, kid string, ct []byte, err error) {
	if len(envelope) < 8 || [4]byte(envelope[:4]) != magic || envelope[4] != envelopeVersion {
		return nil, 0, "", nil, ErrInvalidMessage
	}
	suiteID = envelope[5]
	kidLen := int(envelope[6])
	if kidLen == 0 || len(envelope) < 7+kidLen+1 {
		return nil, 0, "", nil, ErrInvalidMessage
	}
	hdrEnd := 7 + kidLen
	return envelope[:hdrEnd], suiteID, string(envelope[7:hdrEnd]), envelope[hdrEnd:], nil
}
