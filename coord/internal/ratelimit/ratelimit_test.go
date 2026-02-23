package ratelimit

import (
	"testing"
)

func TestAllow(t *testing.T) {
	// 2 requests per second, burst of 3.
	l := New(2, 3)

	// First 3 should be allowed (burst).
	for i := 0; i < 3; i++ {
		if !l.Allow("key1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th should be denied.
	if l.Allow("key1") {
		t.Fatal("4th request should be denied")
	}

	// Different key should work independently.
	if !l.Allow("key2") {
		t.Fatal("different key should be allowed")
	}
}

func TestBurstRefill(t *testing.T) {
	// High rate for testing.
	l := New(1000, 2)

	if !l.Allow("k") {
		t.Fatal("first request should be allowed")
	}
	if !l.Allow("k") {
		t.Fatal("second request should be allowed")
	}

	// Should be denied now.
	if l.Allow("k") {
		t.Fatal("third request should be denied")
	}

	// Manually refill by advancing the bucket's lastTime.
	l.mu.Lock()
	b := l.buckets["k"]
	// Simulate 1 second passing at rate 1000/s -> refills to burst cap.
	b.tokens = float64(l.burst)
	l.mu.Unlock()

	if !l.Allow("k") {
		t.Fatal("should be allowed after refill")
	}
}
