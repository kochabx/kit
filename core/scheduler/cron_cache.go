package scheduler

import (
	"container/list"
	"sync"
	"time"

	"github.com/kochabx/kit/core/scheduler/internal/store"
	"github.com/robfig/cron/v3"
)

type cronCacheKey struct{ expression, location string }
type cronCacheEntry struct {
	key      cronCacheKey
	schedule cron.Schedule
	location *time.Location
}

type cronCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[cronCacheKey]*list.Element
	lru      list.List
}

func newCronCache(capacity int) *cronCache {
	return &cronCache{capacity: capacity, entries: make(map[cronCacheKey]*list.Element)}
}

func (c *cronCache) nextRunMillis(record store.ScheduleRecord, from int64) (int64, error) {
	key := cronCacheKey{record.Expression, record.TimeZone}
	c.mu.Lock()
	if element, ok := c.entries[key]; ok {
		c.lru.MoveToFront(element)
		entry := element.Value.(cronCacheEntry)
		c.mu.Unlock()
		return nextRunMillis(entry.schedule, entry.location, from)
	}
	c.mu.Unlock()
	if key.location == "" {
		return 0, ErrInvalidArgument
	}
	parsed, location, err := parseCron(CronSpec{Expression: key.expression, TimeZone: key.location})
	if err != nil {
		return 0, err
	}
	next, err := nextRunMillis(parsed, location, from)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.lru.MoveToFront(element)
	} else if c.capacity > 0 {
		if c.lru.Len() >= c.capacity {
			oldest := c.lru.Back()
			delete(c.entries, oldest.Value.(cronCacheEntry).key)
			c.lru.Remove(oldest)
		}
		c.entries[key] = c.lru.PushFront(cronCacheEntry{key, parsed, location})
	}
	return next, nil
}
