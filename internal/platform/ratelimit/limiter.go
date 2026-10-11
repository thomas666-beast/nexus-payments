package ratelimit

import (
	"container/list"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is a sharded LRU-cached token bucket limiter.
// Safe for concurrent use.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*list.Element
	order    *list.List
	capacity int

	rate  rate.Limit
	burst int

	// now is injectable for tests.
	now func() time.Time
}

type entry struct {
	key     string
	limiter *rate.Limiter
}

// New creates a limiter with the given per-key rate and burst, and an LRU
// capacity bounding how many distinct keys are tracked.
func New(r rate.Limit, burst, capacity int) *Limiter {
	if capacity <= 0 {
		capacity = 10_000
	}
	return &Limiter{
		buckets:  make(map[string]*list.Element, capacity),
		order:    list.New(),
		capacity: capacity,
		rate:     r,
		burst:    burst,
		now:      time.Now,
	}
}

// Allow reports whether a request for key may proceed now.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	el, ok := l.buckets[key]
	var lim *rate.Limiter
	if !ok {
		lim = rate.NewLimiter(l.rate, l.burst)
		el = l.order.PushFront(&entry{key: key, limiter: lim})
		l.buckets[key] = el
		l.evictIfNeeded()
	} else {
		l.order.MoveToFront(el)
		lim = el.Value.(*entry).limiter
	}

	return lim.AllowN(l.now(), 1)
}

// evictIfNeeded removes the least-recently-used entry if we're over capacity.
func (l *Limiter) evictIfNeeded() {
	for len(l.buckets) > l.capacity {
		back := l.order.Back()
		if back == nil {
			return
		}
		l.order.Remove(back)
		delete(l.buckets, back.Value.(*entry).key)
	}
}

// Size returns the current number of tracked keys. Test-only helper.
func (l *Limiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
