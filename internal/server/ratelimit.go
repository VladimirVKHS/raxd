package server

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// limiterEntry wraps a rate.Limiter with a last-seen timestamp for TTL GC.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Limiters holds per-key and per-IP token-bucket rate limiters.
// All map accesses are protected by mu (SR-18, -race safe).
// A background GC goroutine removes idle entries; it is stopped via ctx in StartGC.
type Limiters struct {
	mu sync.Mutex

	// perKey maps fingerprint → limiter entry.
	perKey map[string]*limiterEntry
	// perIP maps IP string → limiter entry.
	perIP map[string]*limiterEntry

	// Configuration: events per second and burst size.
	ratePerSec float64
	burst      int

	// ttl is the idle time after which an entry is removed by GC.
	ttl time.Duration
}

// NewLimiters creates a Limiters instance with the given rate (events/sec) and burst.
// ttl controls how long idle limiter entries are retained before GC removes them.
func NewLimiters(ratePerSec float64, burst int, ttl time.Duration) *Limiters {
	return &Limiters{
		perKey:     make(map[string]*limiterEntry),
		perIP:      make(map[string]*limiterEntry),
		ratePerSec: ratePerSec,
		burst:      burst,
		ttl:        ttl,
	}
}

// Allow checks both per-key and per-IP limiters for the given fingerprint and IP.
// Returns true if both limiters allow the request; false if either rate limit is exceeded.
// ok==false means the request should be rejected with 429.
// SR-17: per-key AND per-IP, both must pass.
// SR-18: all map access under mu.
func (l *Limiters) Allow(fp, ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Per-key limiter (keyed by fingerprint).
	keyEntry := l.getOrCreate(l.perKey, fp, now)
	if !keyEntry.limiter.Allow() {
		return false
	}

	// Per-IP limiter.
	ipEntry := l.getOrCreate(l.perIP, ip, now)
	if !ipEntry.limiter.Allow() {
		return false
	}

	return true
}

// getOrCreate returns the limiterEntry for key from m, creating it lazily if absent.
// Must be called under l.mu.
func (l *Limiters) getOrCreate(m map[string]*limiterEntry, key string, now time.Time) *limiterEntry {
	e, ok := m[key]
	if !ok {
		e = &limiterEntry{
			limiter:  rate.NewLimiter(rate.Limit(l.ratePerSec), l.burst),
			lastSeen: now,
		}
		m[key] = e
	} else {
		e.lastSeen = now
	}
	return e
}

// StartGC launches a background goroutine that periodically removes idle limiter
// entries from both maps. It stops when ctx is cancelled.
// SR-18: GC goroutine is the only writer that removes entries; reads/writes are under mu.
func (l *Limiters) StartGC(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				l.gc(now)
			}
		}
	}()
}

// gc removes entries from both maps that have not been seen for longer than l.ttl.
// Must be called without holding l.mu (it acquires it internally).
func (l *Limiters) gc(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.perKey {
		if now.Sub(e.lastSeen) > l.ttl {
			delete(l.perKey, k)
		}
	}
	for k, e := range l.perIP {
		if now.Sub(e.lastSeen) > l.ttl {
			delete(l.perIP, k)
		}
	}
}
