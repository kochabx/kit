package rate

import (
	"errors"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestLimitValidate(t *testing.T) {
	t.Parallel()

	if err := PerSecond(10).Validate(); err != nil {
		t.Fatalf("PerSecond(10) failed: %v", err)
	}
	if err := (Limit{}).Validate(); err == nil {
		t.Fatal("expected error for zero Limit")
	}
	if err := (Limit{Rate: 5, Period: time.Second, Burst: -1}).Validate(); err == nil {
		t.Fatal("expected error for negative burst")
	}
}

func TestLocalLimiterAllowBurstDynamicQuotaAndIdleEviction(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0)
	lim := NewLocalLimiter(
		WithClock(func() time.Time { return now }),
		WithMaxEntries(2),
		WithIdleTTL(5*time.Second),
	)

	quota := Limit{Rate: 2, Burst: 2, Period: time.Second}
	for i := range 2 {
		res, err := lim.Allow(t.Context(), "ip:1.1.1.1", quota)
		if err != nil || !res.Allowed {
			t.Fatalf("request %d unexpectedly denied: res=%+v err=%v", i, res, err)
		}
	}

	// 3rd request immediately rejected without consuming budget, with accurate RetryAfter.
	denied, err := lim.Allow(t.Context(), "ip:1.1.1.1", quota)
	if err != nil || denied.Allowed || denied.RetryAfter <= 0 {
		t.Fatalf("expected 3rd request denied with RetryAfter > 0, got %+v (err=%v)", denied, err)
	}

	// Advancing virtual clock refills tokens.
	now = now.Add(600 * time.Millisecond)
	refilled, err := lim.Allow(t.Context(), "ip:1.1.1.1", quota)
	if err != nil || !refilled.Allowed {
		t.Fatalf("expected request allowed after 600ms refill, got %+v (err=%v)", refilled, err)
	}

	// Dynamic quota expansion (rate=5/s, burst=5): previous 1s elapsed at 2/s so tokens=2.2 (now uncapped up to 5).
	expanded := Limit{Rate: 5, Burst: 5, Period: time.Second}
	now = now.Add(time.Second)
	if res, err := lim.AllowN(t.Context(), "ip:1.1.1.1", expanded, 2); err != nil || !res.Allowed {
		t.Fatalf("expected AllowN(2) to switch quota and succeed, got %+v (err=%v)", res, err)
	}
	// Waiting 1s at the expanded 5/s rate refills up to expanded burst=5.
	now = now.Add(time.Second)
	res, err := lim.AllowN(t.Context(), "ip:1.1.1.1", expanded, 4)
	if err != nil || !res.Allowed || res.Remaining != 1 {
		t.Fatalf("expected AllowN(4) under expanded burst=5 to succeed with Remaining=1, got %+v (err=%v)", res, err)
	}

	// Idle TTL resets bucket cleanly.
	now = now.Add(6 * time.Second)
	afterIdle, err := lim.AllowN(t.Context(), "ip:1.1.1.1", expanded, 5)
	if err != nil || !afterIdle.Allowed {
		t.Fatalf("expected full fresh bucket after IdleTTL, got %+v (err=%v)", afterIdle, err)
	}
}

func TestRedisLimiterValidationAndKeyFormatting(t *testing.T) {
	t.Parallel()

	if _, err := NewRedisLimiter(nil); err == nil {
		t.Fatal("expected error for nil redis client")
	}

	rdb := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	defer func() { _ = rdb.Close() }()

	rl, err := NewRedisLimiter(rdb, WithRedisPrefix("api"))
	if err != nil {
		t.Fatalf("NewRedisLimiter failed: %v", err)
	}
	if got := rl.formatKey("user:42"); got != "api:user:42" {
		t.Fatalf("formatKey = %q, want api:user:42", got)
	}
	if _, err := rl.AllowN(t.Context(), "", PerMinute(10), 1); err == nil {
		t.Fatal("expected error for empty key")
	}
	if _, err := rl.AllowN(t.Context(), "user:42", PerSecond(2), 3); !errors.Is(err, ErrExceedsBurst) {
		t.Fatalf("RedisLimiter AllowN(n > burst) = %v, want ErrExceedsBurst", err)
	}
}

func TestRateContractResetAfterAndEvictionPrefersFullQuota(t *testing.T) {
	t.Parallel()

	now := time.Unix(2000, 0)
	lim := NewLocalLimiter(
		WithClock(func() time.Time { return now }),
		WithMaxEntries(2),
		WithIdleTTL(10*time.Second),
	)

	// 1. n > burst returns ErrExceedsBurst uniformly without allocating a bucket.
	quota := Limit{Rate: 2, Burst: 2, Period: time.Second}
	if _, err := lim.AllowN(t.Context(), "ip:burst", quota, 3); !errors.Is(err, ErrExceedsBurst) {
		t.Fatalf("LocalLimiter AllowN(n > burst) = %v, want ErrExceedsBurst", err)
	}

	// 2. Allowed request reports ResetAfter > 0 until full burst is restored; denied reports RetryAfter < ResetAfter.
	res1, err := lim.Allow(t.Context(), "ip:1", quota)
	if err != nil || !res1.Allowed || res1.RetryAfter != 0 || res1.ResetAfter != 500*time.Millisecond {
		t.Fatalf("first allow = %+v (err=%v), want Allowed with ResetAfter=500ms", res1, err)
	}
	res2, err := lim.Allow(t.Context(), "ip:1", quota)
	if err != nil || !res2.Allowed || res2.ResetAfter != time.Second {
		t.Fatalf("second allow = %+v (err=%v), want Allowed with ResetAfter=1s", res2, err)
	}
	denied, err := lim.Allow(t.Context(), "ip:1", quota)
	if err != nil || denied.Allowed || denied.RetryAfter != 500*time.Millisecond || denied.ResetAfter != time.Second {
		t.Fatalf("denied = %+v (err=%v), want RetryAfter=500ms ResetAfter=1s", denied, err)
	}

	// 3. Eviction under capacity pressure evicts full-quota idle buckets before throttled buckets.
	if _, err := lim.Allow(t.Context(), "ip:2", quota); err != nil {
		t.Fatal(err)
	}
	// Advance 250ms: ip:1 still owes 750ms (throttled, not full); keep it depleted while ip:2 recovers at +1s.
	now = now.Add(time.Second)
	// ip:2 now has full burst (2/2), while ip:1 consumes 2 again so ip:1 is depleted (0/2).
	if res, err := lim.AllowN(t.Context(), "ip:1", quota, 2); err != nil || !res.Allowed {
		t.Fatalf("deplete ip:1 = %+v (err=%v)", res, err)
	}
	// Trigger eviction by inserting ip:3 at maxEntries=2: ip:2 (full quota) should be evicted, preserving ip:1 throttle.
	if _, err := lim.Allow(t.Context(), "ip:3", quota); err != nil {
		t.Fatal(err)
	}
	stillThrottled, err := lim.Allow(t.Context(), "ip:1", quota)
	if err != nil || stillThrottled.Allowed {
		t.Fatalf("throttled bucket ip:1 was evicted instead of full-quota ip:2: %+v (err=%v)", stillThrottled, err)
	}
}

func TestLocalLimiterLongPeriodNotResetEarlyByIdleTTL(t *testing.T) {
	t.Parallel()

	now := time.Unix(3000, 0)
	lim := NewLocalLimiter(
		WithClock(func() time.Time { return now }),
		WithIdleTTL(10*time.Minute),
	)

	hourly := PerHour(1)
	res0, err := lim.Allow(t.Context(), "user:hourly", hourly)
	if err != nil || !res0.Allowed {
		t.Fatalf("t=0 Allow = (%+v, %v), want Allowed", res0, err)
	}

	// After 10m (= IdleTTL), the bucket still owes 50m and must NOT be expired or reset.
	now = now.Add(10 * time.Minute)
	res10, err := lim.Allow(t.Context(), "user:hourly", hourly)
	if err != nil || res10.Allowed || res10.RetryAfter != 50*time.Minute {
		t.Fatalf("t=10m Allow = (%+v, %v), want denied with RetryAfter=50m", res10, err)
	}

	// After a full 60m, one token is refilled and Allow succeeds.
	now = now.Add(50 * time.Minute)
	res60, err := lim.Allow(t.Context(), "user:hourly", hourly)
	if err != nil || !res60.Allowed {
		t.Fatalf("t=60m Allow = (%+v, %v), want Allowed", res60, err)
	}
}
