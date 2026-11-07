package repository

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterBlocksPastLimitAndResetsAfterWindow(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	key := "rl:test:" + uniqueMockID(t)
	t.Cleanup(func() { rdb.Del(context.Background(), key) })
	l := NewRateLimiter(rdb, 2, time.Second)

	for i, wantAllowed := range []bool{true, true, false} {
		dec, err := l.Allow(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if dec.Allowed != wantAllowed {
			t.Fatalf("call %d: allowed = %v, want %v", i+1, dec.Allowed, wantAllowed)
		}
		if !wantAllowed && dec.RetryAfter <= 0 {
			t.Fatalf("blocked call has RetryAfter %v", dec.RetryAfter)
		}
	}
	time.Sleep(1100 * time.Millisecond)
	if dec, err := l.Allow(ctx, key); err != nil || !dec.Allowed {
		t.Fatalf("after the window: allowed = %v, err = %v", dec.Allowed, err)
	}
}

// A counter left without a TTL (process died between INCR and EXPIRE) would
// block its IP forever; the next call has to give it one.
func TestRateLimiterHealsCounterWithoutTTL(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	key := "rl:test:" + uniqueMockID(t)
	t.Cleanup(func() { rdb.Del(context.Background(), key) })
	if err := rdb.Set(ctx, key, 100, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRateLimiter(rdb, 2, time.Minute).Allow(ctx, key); err != nil {
		t.Fatal(err)
	}
	if ttl := rdb.TTL(ctx, key).Val(); ttl <= 0 {
		t.Fatalf("TTL = %v, want the window", ttl)
	}
}
