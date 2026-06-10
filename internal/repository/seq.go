package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// SeqCounter hands out per-mock, monotonically increasing hit positions for
// response sequences. Redis INCR keeps the position shared across callers
// and instances; the position is "cheap to lose" state (a Redis restart
// resets every sequence to step 1, which is acceptable — see
// ARCHITECTURE.md on what Redis may hold).
//
// When Redis is unavailable the counter falls back to an in-process atomic,
// so cycling keeps working within this instance instead of pinning every
// caller to step 1.
type SeqCounter struct {
	rdb *redis.Client // nil ⇒ memory-only (tests)
	ttl time.Duration
	mem sync.Map // mock ID → *atomic.Uint64
}

// NewSeqCounter wraps rdb. The key TTL slides on every hit; a week outlives
// any realistic test session while still cleaning up after expired mocks.
func NewSeqCounter(rdb *redis.Client) *SeqCounter {
	return &SeqCounter{rdb: rdb, ttl: 7 * 24 * time.Hour}
}

// Next returns the 0-based position for this hit of mockID.
func (c *SeqCounter) Next(ctx context.Context, mockID string) uint64 {
	if c.rdb != nil {
		key := "seq:" + mockID
		pipe := c.rdb.TxPipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, c.ttl)
		if _, err := pipe.Exec(ctx); err == nil {
			return uint64(incr.Val()) - 1
		}
	}
	v, _ := c.mem.LoadOrStore(mockID, &atomic.Uint64{})
	return v.(*atomic.Uint64).Add(1) - 1
}
