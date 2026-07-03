package api

import (
	"testing"
	"time"
)

func TestRateLimiterBurstAndRefill(t *testing.T) {
	now := time.Unix(1000, 0)
	rl := newRateLimiter(1, 3) // 1 token/s, burst 3
	rl.now = func() time.Time { return now }

	for i := range 3 {
		if !rl.allow("ip1") {
			t.Fatalf("request %d within burst was denied", i)
		}
	}
	if rl.allow("ip1") {
		t.Fatal("request beyond burst was allowed")
	}
	// A different key has its own bucket.
	if !rl.allow("ip2") {
		t.Fatal("independent key was denied")
	}

	now = now.Add(2 * time.Second) // refills 2 tokens
	if !rl.allow("ip1") || !rl.allow("ip1") {
		t.Fatal("refilled tokens were denied")
	}
	if rl.allow("ip1") {
		t.Fatal("exceeded refill was allowed")
	}
}
