// Package ratelimit provides per-key token-bucket rate limiting.
package ratelimit

import (
	"sync"
	"time"
)

// maxBuckets caps the total number of distinct keys tracked. Prevents a
// memory-exhaustion attack from a client that rotates identities rapidly.
// When the cap is reached, the least-recently-used bucket is evicted.
const maxBuckets = 100_000

// Limiter implements per-key token-bucket rate limiting.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64       // tokens per second
	burst   int           // max tokens (bucket capacity)
	cleanup time.Duration // how often to prune stale buckets
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

// New creates a rate limiter that allows `rate` requests per second with a burst of `burst`.
func New(rate float64, burst int) *Limiter {
	l := &Limiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		cleanup: 5 * time.Minute,
	}
	go l.cleanupLoop()
	return l
}

// Allow checks if a request from `key` should be allowed.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			// Evict the least-recently-used bucket to make room.
			var oldestKey string
			var oldestTime time.Time
			first := true
			for k, v := range l.buckets {
				if first || v.lastTime.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.lastTime
					first = false
				}
			}
			delete(l.buckets, oldestKey)
		}
		b = &bucket{
			tokens:   float64(l.burst) - 1,
			lastTime: now,
		}
		l.buckets[key] = b
		return true
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastTime = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}

// cleanupLoop periodically removes stale buckets.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		cutoff := time.Now().Add(-l.cleanup)
		for key, b := range l.buckets {
			if b.lastTime.Before(cutoff) {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}
