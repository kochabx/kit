package mfa

import (
	"cmp"
	"container/heap"
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"

	"github.com/kochabx/kit/fault"
)

const (
	defaultPeriod         uint = 30
	defaultSecretSize     uint = 20
	defaultHOTPSecretSize uint = 20
	defaultReplayMaxCap        = 10000
)

// ErrReplayStoreFull indicates the bounded in-memory replay store cannot admit
// another active entry until existing entries expire.
var ErrReplayStoreFull = fault.ServiceUnavailable("mfa: replay store capacity exhausted")

// ReplayStore prevents reuse of already-accepted OTP codes within their validity window.
type ReplayStore interface {
	Use(ctx context.Context, key string, ttl time.Duration) (firstUse bool, err error)
}

type replayHeapItem struct {
	key       string
	expiresAt time.Time
}

type replayHeap []replayHeapItem

func (h replayHeap) Len() int           { return len(h) }
func (h replayHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h replayHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *replayHeap) Push(x any)        { *h = append(*h, x.(replayHeapItem)) }
func (h *replayHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = replayHeapItem{}
	*h = old[:n-1]
	return item
}

// MemoryReplayStore is a bounded concurrency-safe ReplayStore suitable for single-node deployments and tests.
type MemoryReplayStore struct {
	mu      sync.Mutex
	maxCap  int
	clock   func() time.Time
	entries map[string]time.Time
	expiry  replayHeap
}

// NewMemoryReplayStore constructs a MemoryReplayStore capped at maxEntries (default 10000).
func NewMemoryReplayStore(maxEntries int, clock ...func() time.Time) *MemoryReplayStore {
	clk := func() time.Time { return time.Now().UTC() }
	if len(clock) > 0 && clock[0] != nil {
		clk = clock[0]
	}
	return &MemoryReplayStore{
		maxCap:  cmp.Or(maxEntries, defaultReplayMaxCap),
		clock:   clk,
		entries: make(map[string]time.Time),
	}
}

// Use records key for ttl and returns true if key has not been used yet.
// Unexpired entries are never evicted early; if capacity is full of live entries, ErrReplayStoreFull is returned.
func (s *MemoryReplayStore) Use(_ context.Context, key string, ttl time.Duration) (bool, error) {
	if key == "" || ttl <= 0 {
		return false, fault.InvalidArgument("invalid replay key or ttl")
	}

	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.expiry) > 0 && !now.Before(s.expiry[0].expiresAt) {
		item := heap.Pop(&s.expiry).(replayHeapItem)
		if cur, ok := s.entries[item.key]; ok && cur.Equal(item.expiresAt) {
			delete(s.entries, item.key)
		}
	}

	if _, ok := s.entries[key]; ok {
		return false, nil
	}
	if len(s.entries) >= s.maxCap {
		return false, ErrReplayStoreFull
	}

	exp := now.Add(ttl)
	s.entries[key] = exp
	heap.Push(&s.expiry, replayHeapItem{key: key, expiresAt: exp})
	return true, nil
}

// Enrollment contains newly generated OTP provisioning details.
type Enrollment struct {
	Secret  string `json:"secret"`
	URI     string `json:"uri"`
	Issuer  string `json:"issuer"`
	Account string `json:"account"`
}

// TOTPConfig configures a TOTP instance.
type TOTPConfig struct {
	Issuer     string           `json:"issuer"`
	Period     uint             `json:"period"`
	Digits     otp.Digits       `json:"digits"`
	Algorithm  otp.Algorithm    `json:"algorithm"`
	Skew       uint             `json:"skew"`
	SecretSize uint             `json:"secret_size"`
	Clock      func() time.Time `json:"-"`
}

// Validate checks TOTPConfig.
func (c TOTPConfig) Validate() error {
	var errs []error
	if c.Issuer == "" {
		errs = append(errs, errors.New("mfa: issuer is required"))
	}
	if c.Skew > 10 {
		errs = append(errs, errors.New("mfa: skew cannot exceed 10"))
	}
	return errors.Join(errs...)
}

// Option configures a TOTP instance.
type Option func(*totpOptions)

type totpOptions struct {
	replayStore ReplayStore
}

// WithReplayStore overrides the default ReplayStore for TOTP replay protection.
// Pass nil explicitly to disable replay tracking (not recommended for production).
func WithReplayStore(store ReplayStore) Option {
	return func(o *totpOptions) {
		o.replayStore = store
	}
}

// TOTP generates and verifies Time-based One-Time Passwords with constant-time comparison and replay protection.
type TOTP struct {
	issuer     string
	period     uint
	digits     otp.Digits
	algorithm  otp.Algorithm
	skew       uint
	secretSize uint
	clock      func() time.Time
	replay     ReplayStore
}

// NewTOTP constructs a TOTP verifier/generator without global singletons.
func NewTOTP(cfg TOTPConfig, opts ...Option) (*TOTP, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fault.InvalidArgument("invalid totp config").WithCause(err)
	}

	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	o := totpOptions{
		replayStore: NewMemoryReplayStore(defaultReplayMaxCap, clock),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	digits := cfg.Digits
	if digits == 0 {
		digits = otp.DigitsSix
	}
	return &TOTP{
		issuer:     cfg.Issuer,
		period:     cmp.Or(cfg.Period, defaultPeriod),
		digits:     digits,
		algorithm:  cfg.Algorithm,
		skew:       cfg.Skew,
		secretSize: cmp.Or(cfg.SecretSize, defaultSecretSize),
		clock:      clock,
		replay:     o.replayStore,
	}, nil
}

// Generate creates a fresh cryptographic TOTP secret and otpauth:// enrollment URI for account.
func (t *TOTP) Generate(account string) (Enrollment, error) {
	if account == "" {
		return Enrollment{}, fault.InvalidArgument("account is required")
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      t.issuer,
		AccountName: account,
		Period:      t.period,
		SecretSize:  t.secretSize,
		Digits:      t.digits,
		Algorithm:   t.algorithm,
	})
	if err != nil {
		return Enrollment{}, fault.Internal("failed to generate totp secret").WithCause(err)
	}
	return Enrollment{
		Secret:  key.Secret(),
		URI:     key.URL(),
		Issuer:  t.issuer,
		Account: account,
	}, nil
}

// Verify checks code against secret in constant time and enforces single-use per step via ReplayStore.
func (t *TOTP) Verify(ctx context.Context, account, secret, code string) error {
	if account == "" || secret == "" || code == "" {
		return fault.InvalidArgument("account, secret, and code are required")
	}

	now := t.clock().UTC()
	baseStep := int64(math.Floor(float64(now.Unix()) / float64(t.period)))

	steps := make([]int64, 0, 1+2*t.skew)
	steps = append(steps, baseStep)
	for i := range t.skew {
		offset := int64(i + 1)
		steps = append(steps, baseStep+offset, baseStep-offset)
	}

	valOpts := hotp.ValidateOpts{
		Digits:    t.digits,
		Algorithm: t.algorithm,
	}

	matchedStep := int64(-1)
	matched := false
	for _, step := range steps {
		if step < 0 {
			continue
		}
		ok, err := hotp.ValidateCustom(code, uint64(step), secret, valOpts)
		if err != nil {
			return fault.InvalidArgument("invalid totp code or secret").WithCause(err)
		}
		if ok {
			matched = true
			matchedStep = step
			break
		}
	}
	if !matched {
		return fault.Unauthorized("invalid otp code")
	}

	if t.replay != nil {
		replayKey := account + ":" + strconv.FormatInt(matchedStep, 10)
		replayTTL := time.Duration(2*t.skew+1) * time.Duration(t.period) * time.Second
		firstUse, err := t.replay.Use(ctx, replayKey, replayTTL)
		if err != nil {
			if f, ok := errors.AsType[*fault.Error](err); ok {
				return f
			}
			return fault.Internal("failed to check otp replay store").WithCause(err)
		}
		if !firstUse {
			return fault.Unauthorized("otp code already used")
		}
	}
	return nil
}

// HOTPConfig configures an HOTP instance.
type HOTPConfig struct {
	Issuer     string        `json:"issuer"`
	Digits     otp.Digits    `json:"digits"`
	Algorithm  otp.Algorithm `json:"algorithm"`
	SecretSize uint          `json:"secret_size"`
}

// Validate checks HOTPConfig.
func (c HOTPConfig) Validate() error {
	if c.Issuer == "" {
		return errors.New("mfa: issuer is required")
	}
	return nil
}

// HOTP generates and verifies HMAC-based One-Time Passwords.
type HOTP struct {
	issuer     string
	digits     otp.Digits
	algorithm  otp.Algorithm
	secretSize uint
}

// NewHOTP constructs an HOTP generator/verifier.
func NewHOTP(cfg HOTPConfig) (*HOTP, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fault.InvalidArgument("invalid hotp config").WithCause(err)
	}
	digits := cfg.Digits
	if digits == 0 {
		digits = otp.DigitsSix
	}
	return &HOTP{
		issuer:     cfg.Issuer,
		digits:     digits,
		algorithm:  cfg.Algorithm,
		secretSize: cmp.Or(cfg.SecretSize, defaultHOTPSecretSize),
	}, nil
}

// Generate creates a fresh cryptographic HOTP secret and otpauth:// enrollment URI for account.
func (h *HOTP) Generate(account string) (Enrollment, error) {
	if account == "" {
		return Enrollment{}, fault.InvalidArgument("account is required")
	}
	key, err := hotp.Generate(hotp.GenerateOpts{
		Issuer:      h.issuer,
		AccountName: account,
		SecretSize:  h.secretSize,
		Digits:      h.digits,
		Algorithm:   h.algorithm,
	})
	if err != nil {
		return Enrollment{}, fault.Internal("failed to generate hotp secret").WithCause(err)
	}
	return Enrollment{
		Secret:  key.Secret(),
		URI:     key.URL(),
		Issuer:  h.issuer,
		Account: account,
	}, nil
}

// Verify checks code against secret and counter in constant time.
func (h *HOTP) Verify(secret, code string, counter uint64) error {
	if secret == "" || code == "" {
		return fault.InvalidArgument("secret and code are required")
	}
	ok, err := hotp.ValidateCustom(code, counter, secret, hotp.ValidateOpts{
		Digits:    h.digits,
		Algorithm: h.algorithm,
	})
	if err != nil {
		return fault.InvalidArgument("invalid hotp code or secret").WithCause(err)
	}
	if !ok {
		return fault.Unauthorized("invalid otp code")
	}
	return nil
}
