package repository

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestSeqCounterMemory(t *testing.T) {
	c := NewSeqCounter(nil) // nil client ⇒ memory-only
	ctx := context.Background()

	for want := uint64(0); want < 5; want++ {
		if got := c.Next(ctx, "mock-a"); got != want {
			t.Fatalf("Next(mock-a) = %d, want %d", got, want)
		}
	}
	if got := c.Next(ctx, "mock-b"); got != 0 {
		t.Fatalf("counters must be independent per mock: got %d, want 0", got)
	}
}

func TestSeqCounterFallsBackWhenRedisDown(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listens here
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer rdb.Close()

	c := NewSeqCounter(rdb)
	ctx := context.Background()
	if got := c.Next(ctx, "mock-a"); got != 0 {
		t.Fatalf("first fallback Next = %d, want 0", got)
	}
	if got := c.Next(ctx, "mock-a"); got != 1 {
		t.Fatalf("second fallback Next = %d, want 1", got)
	}
}
