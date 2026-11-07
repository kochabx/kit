package scheduler

import (
	"testing"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
)

func TestCronCacheEvictionAndTimezone(t *testing.T) {
	cache := newCronCache(2)
	from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC).UnixMilli()
	records := []store.ScheduleRecord{{Expression: "0 9 * * *", TimeZone: "Asia/Shanghai"}, {Expression: "0 9 * * *", TimeZone: "UTC"}, {Expression: "0 10 * * *", TimeZone: "UTC"}}
	for _, record := range records {
		want, err := nextCronFrom(record, from)
		if err != nil {
			t.Fatal(err)
		}
		got, err := cache.nextRunMillis(record, from)
		if err != nil || got != want {
			t.Fatalf("cached next=%d want=%d err=%v", got, want, err)
		}
	}
	if len(cache.entries) != 2 {
		t.Fatalf("unbounded cache size=%d", len(cache.entries))
	}
	if _, ok := cache.entries[cronCacheKey{records[0].Expression, records[0].TimeZone}]; ok {
		t.Fatal("oldest entry not evicted")
	}
	for _, record := range []store.ScheduleRecord{{Expression: "invalid", TimeZone: "UTC"}, {Expression: "@every 1h", TimeZone: "invalid/zone"}} {
		if _, err := cache.nextRunMillis(record, from); err == nil {
			t.Fatal("invalid schedule cached")
		}
	}
	if len(cache.entries) != 2 {
		t.Fatal("failed parse evicted valid cache entry")
	}
}

func nextCronFrom(record store.ScheduleRecord, from int64) (int64, error) {
	parsed, location, err := parseCron(CronSpec{Expression: record.Expression, TimeZone: record.TimeZone})
	if err != nil {
		return 0, err
	}
	return nextRunMillis(parsed, location, from)
}

type blockingCron struct {
	entered chan struct{}
	release chan struct{}
}

func (s blockingCron) Next(t time.Time) time.Time {
	close(s.entered)
	<-s.release
	return t.Add(time.Hour)
}

func TestCronNextCalculationDoesNotHoldCacheLock(t *testing.T) {
	cache := newCronCache(2)
	blocker := blockingCron{make(chan struct{}), make(chan struct{})}
	defer close(blocker.release)
	key := cronCacheKey{"blocked", "UTC"}
	cache.entries[key] = cache.lru.PushFront(cronCacheEntry{key: key, schedule: blocker, location: time.UTC})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.nextRunMillis(store.ScheduleRecord{Expression: key.expression, TimeZone: key.location}, time.Now().UnixMilli())
	}()
	select {
	case <-blocker.entered:
	case <-time.After(time.Second):
		t.Fatal("blocked calculation did not start")
	}
	other := make(chan error, 1)
	go func() {
		_, err := cache.nextRunMillis(store.ScheduleRecord{Expression: "@every 1h", TimeZone: "UTC"}, time.Now().UnixMilli())
		other <- err
	}()
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("another entry was blocked by Next")
	}
}
