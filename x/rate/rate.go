package rate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-redis/redis_rate/v10"
	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// ErrExceedsBurst indicates n exceeds the quota's effective burst capacity.
var ErrExceedsBurst = errors.New("rate: requested tokens exceed burst")

// Limit defines a rate-limit quota over Period with optional Burst capacity (defaults to Rate).
type Limit struct {
	Rate   int           `json:"rate"`
	Burst  int           `json:"burst"`
	Period time.Duration `json:"period"`
}

// PerSecond returns a quota of n events per second with burst n.
func PerSecond(n int) Limit {
	return Limit{Rate: n, Burst: n, Period: time.Second}
}

// PerMinute returns a quota of n events per minute with burst n.
func PerMinute(n int) Limit {
	return Limit{Rate: n, Burst: n, Period: time.Minute}
}

// PerHour returns a quota of n events per hour with burst n.
func PerHour(n int) Limit {
	return Limit{Rate: n, Burst: n, Period: time.Hour}
}

// Validate checks Limit fields.
func (l Limit) Validate() error {
	var errs []error
	if l.Rate <= 0 {
		errs = append(errs, errors.New("rate: limit.rate must be positive"))
	}
	if l.Period <= 0 {
		errs = append(errs, errors.New("rate: limit.period must be positive"))
	}
	if l.Burst < 0 {
		errs = append(errs, errors.New("rate: limit.burst cannot be negative"))
	}
	return errors.Join(errs...)
}

func (l Limit) effectiveBurst() int {
	return cmp.Or(l.Burst, l.Rate)
}

func validateAllowRequest(key string, limit Limit, n int) (int, error) {
	if key == "" {
		return 0, errors.New("rate: key is required")
	}
	if n <= 0 {
		return 0, errors.New("rate: n must be positive")
	}
	if err := limit.Validate(); err != nil {
		return 0, err
	}
	burst := limit.effectiveBurst()
	if n > burst {
		return 0, ErrExceedsBurst
	}
	return burst, nil
}

// Result describes the decision and remaining budget of a rate-limit check.
type Result struct {
	Allowed    bool          `json:"allowed"`
	Limit      int           `json:"limit"`
	Remaining  int           `json:"remaining"`
	RetryAfter time.Duration `json:"retry_after"`
	ResetAfter time.Duration `json:"reset_after"`
}

// Limiter checks whether key may consume tokens under dynamic quota limit.
type Limiter interface {
	Allow(ctx context.Context, key string, limit Limit) (Result, error)
	AllowN(ctx context.Context, key string, limit Limit, n int) (Result, error)
}

// RedisOption configures a RedisLimiter.
type RedisOption func(*RedisLimiter)

// WithRedisPrefix sets an optional namespace prefix separated from key by ':'.
func WithRedisPrefix(prefix string) RedisOption {
	return func(l *RedisLimiter) {
		l.prefix = prefix
	}
}

// RedisLimiter implements Limiter backed by O(1) Redis GCRA via go-redis/redis_rate/v10.
type RedisLimiter struct {
	limiter *redis_rate.Limiter
	prefix  string
}

var _ Limiter = (*RedisLimiter)(nil)

// NewRedisLimiter wraps a go-redis UniversalClient with O(1) GCRA distributed rate limiting.
func NewRedisLimiter(rdb goredis.UniversalClient, opts ...RedisOption) (*RedisLimiter, error) {
	if rdb == nil {
		return nil, errors.New("rate: redis client is required")
	}
	rl := &RedisLimiter{
		limiter: redis_rate.NewLimiter(rdb),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(rl)
		}
	}
	return rl, nil
}

func (l *RedisLimiter) formatKey(key string) string {
	if l.prefix == "" {
		return key
	}
	return l.prefix + ":" + key
}

// Allow consumes 1 token for key under limit.
func (l *RedisLimiter) Allow(ctx context.Context, key string, limit Limit) (Result, error) {
	return l.AllowN(ctx, key, limit, 1)
}

// AllowN attempts to consume n tokens for key under limit via O(1) GCRA.
func (l *RedisLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (Result, error) {
	burst, err := validateAllowRequest(key, limit, n)
	if err != nil {
		return Result{}, err
	}

	rrLimit := redis_rate.Limit{
		Rate:   limit.Rate,
		Burst:  burst,
		Period: limit.Period,
	}
	res, err := l.limiter.AllowN(ctx, l.formatKey(key), rrLimit, n)
	if err != nil {
		return Result{}, fmt.Errorf("rate: redis allow: %w", err)
	}
	return Result{
		Allowed:    res.Allowed >= n,
		Limit:      res.Limit.Rate,
		Remaining:  res.Remaining,
		RetryAfter: max(0, res.RetryAfter),
		ResetAfter: max(0, res.ResetAfter),
	}, nil
}

const (
	defaultLocalMaxEntries = 10000
	defaultLocalIdleTTL    = 10 * time.Minute
)

// LocalOption configures a LocalLimiter.
type LocalOption func(*LocalLimiter)

// WithClock overrides the clock used by LocalLimiter (useful in deterministic tests).
func WithClock(clock func() time.Time) LocalOption {
	return func(l *LocalLimiter) {
		if clock != nil {
			l.clock = clock
		}
	}
}

// WithMaxEntries bounds the maximum tracked keys in LocalLimiter (default 10000).
func WithMaxEntries(n int) LocalOption {
	return func(l *LocalLimiter) {
		if n > 0 {
			l.maxEntries = n
		}
	}
}

// WithIdleTTL sets the idle expiration duration before unused key buckets are evicted (default 10m).
func WithIdleTTL(d time.Duration) LocalOption {
	return func(l *LocalLimiter) {
		if d > 0 {
			l.idleTTL = d
		}
	}
}

type localBucket struct {
	limiter  *rate.Limiter
	rps      rate.Limit
	burst    int
	lastSeen time.Time
}

func (b *localBucket) isIdleExpired(now time.Time, idleTTL time.Duration) bool {
	return now.Sub(b.lastSeen) >= idleTTL && b.limiter.TokensAt(now) >= float64(b.burst)
}

// LocalLimiter is a bounded per-key in-memory Limiter backed by golang.org/x/time/rate.
type LocalLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*localBucket
	maxEntries int
	idleTTL    time.Duration
	clock      func() time.Time
}

var _ Limiter = (*LocalLimiter)(nil)

// NewLocalLimiter constructs a bounded in-memory token-bucket Limiter supporting dynamic quotas.
func NewLocalLimiter(opts ...LocalOption) *LocalLimiter {
	l := &LocalLimiter{
		buckets:    make(map[string]*localBucket),
		maxEntries: defaultLocalMaxEntries,
		idleTTL:    defaultLocalIdleTTL,
		clock:      func() time.Time { return time.Now() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}

// Allow consumes 1 token for key under limit.
func (l *LocalLimiter) Allow(ctx context.Context, key string, limit Limit) (Result, error) {
	return l.AllowN(ctx, key, limit, 1)
}

// AllowN attempts to consume n tokens for key under limit.
func (l *LocalLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	burst, err := validateAllowRequest(key, limit, n)
	if err != nil {
		return Result{}, err
	}

	rps := rate.Limit(float64(limit.Rate) / limit.Period.Seconds())
	now := l.clock()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if ok && b.isIdleExpired(now, l.idleTTL) {
		delete(l.buckets, key)
		ok = false
	}
	if !ok {
		if len(l.buckets) >= l.maxEntries {
			l.evict(now)
		}
		b = &localBucket{
			limiter: rate.NewLimiter(rps, burst),
			rps:     rps,
			burst:   burst,
		}
		l.buckets[key] = b
	} else {
		if b.rps != rps {
			b.limiter.SetLimitAt(now, rps)
			b.rps = rps
		}
		if b.burst != burst {
			b.limiter.SetBurstAt(now, burst)
			b.burst = burst
		}
	}
	b.lastSeen = now

	resv := b.limiter.ReserveN(now, n)
	delay := resv.DelayFrom(now)
	if delay > 0 {
		resv.CancelAt(now)
		tokens := b.limiter.TokensAt(now)
		return Result{
			Allowed:    false,
			Limit:      limit.Rate,
			Remaining:  max(0, int(tokens)),
			RetryAfter: delay,
			ResetAfter: refillDuration(burst, tokens, rps),
		}, nil
	}

	tokens := b.limiter.TokensAt(now)
	return Result{
		Allowed:    true,
		Limit:      limit.Rate,
		Remaining:  max(0, int(tokens)),
		RetryAfter: 0,
		ResetAfter: refillDuration(burst, tokens, rps),
	}, nil
}

func refillDuration(burst int, tokens float64, rps rate.Limit) time.Duration {
	deficit := float64(burst) - tokens
	if deficit <= 0 || rps <= 0 {
		return 0
	}
	return time.Duration(math.Ceil(deficit / float64(rps) * float64(time.Second)))
}

const evictSampleSize = 64

func (l *LocalLimiter) evict(now time.Time) {
	var (
		fullKey    string
		oldestKey  string
		oldestSeen time.Time
		sampled    int
	)
	for k, b := range l.buckets {
		sampled++
		if b.isIdleExpired(now, l.idleTTL) {
			delete(l.buckets, k)
		} else {
			if fullKey == "" && b.limiter.TokensAt(now) >= float64(b.burst) {
				fullKey = k
			}
			if oldestKey == "" || b.lastSeen.Before(oldestSeen) {
				oldestKey = k
				oldestSeen = b.lastSeen
			}
		}
		if sampled >= evictSampleSize {
			break
		}
	}
	if len(l.buckets) < l.maxEntries {
		return
	}
	if fullKey != "" {
		delete(l.buckets, fullKey)
		return
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}
