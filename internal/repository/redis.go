package repository

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a fixed-window per-key counter on top of Redis.
//
// Algorithm: INCR key; on first increment, set TTL = window. Subsequent
// increments do not reset the window. When the counter exceeds the limit,
// the caller is denied. This is intentionally simple — sliding-window or
// token-bucket are overkill for the scale at MVP.
type RateLimiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func NewRateLimiter(rdb *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{rdb: rdb, limit: limit, window: window}
}

// Decision is what Allow returns.
type Decision struct {
	Allowed    bool
	Remaining  int           // requests left in the window after this call
	RetryAfter time.Duration // 0 if Allowed
}

// Allow consumes one unit of quota for `key`. The key is typically
// "rl:ip:<ip>" or "rl:api:<ip>".
func (r *RateLimiter) Allow(ctx context.Context, key string) (Decision, error) {
	// Pipeline INCR + TTL so we get both with one round-trip after the
	// first call. On the very first call we set the TTL.
	pipe := r.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	ttl := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return Decision{}, err
	}

	n := incr.Val()
	remaining := r.limit - int(n)
	if remaining < 0 {
		remaining = 0
	}

	// First increment in this window — set the expiry.
	if ttl.Val() < 0 {
		_ = r.rdb.Expire(ctx, key, r.window).Err()
	}

	if int(n) > r.limit {
		retry := ttl.Val()
		if retry <= 0 {
			retry = r.window
		}
		return Decision{Allowed: false, Remaining: 0, RetryAfter: retry}, nil
	}
	return Decision{Allowed: true, Remaining: remaining}, nil
}

// Ping verifies connectivity, used by /healthz.
func (r *RateLimiter) Ping(ctx context.Context) error {
	return r.rdb.Ping(ctx).Err()
}
