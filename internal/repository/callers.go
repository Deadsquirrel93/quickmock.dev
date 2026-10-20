package repository

import (
	"context"
	"net/netip"
	"time"

	"github.com/redis/go-redis/v9"
)

// CallerTracker counts the distinct callers of each mock per UTC day in a
// Redis HyperLogLog. A test mock has a handful of callers; dozens mean it
// is serving someone's production traffic (the 2026-10-20 audit found one
// acting as a license server for a third-party app).
//
// The sketch holds hashed registers, not addresses, and expires on its own,
// so it stays within the "IPs are operational" rule in ARCHITECTURE.md.
type CallerTracker struct {
	rdb       *redis.Client // nil ⇒ disabled (tests)
	threshold int64
	ttl       time.Duration
}

// NewCallerTracker reports a mock once per day when its distinct callers
// reach threshold. Keys live 48h so a day's sketch outlasts the day itself.
func NewCallerTracker(rdb *redis.Client, threshold int64) *CallerTracker {
	return &CallerTracker{rdb: rdb, threshold: threshold, ttl: 48 * time.Hour}
}

// Observe records ip as today's caller of mockID and returns the distinct
// caller estimate. crossed is true exactly once per mock per day: on the
// call that brings the estimate to the threshold. Redis errors yield
// (0, false) — the signal is best-effort and must never fail a mock.
func (t *CallerTracker) Observe(ctx context.Context, mockID, ip string) (callers int64, crossed bool) {
	if t == nil || t.rdb == nil {
		return 0, false
	}
	key := "callers:" + mockID + ":" + time.Now().UTC().Format("20060102")
	pipe := t.rdb.TxPipeline()
	added := pipe.PFAdd(ctx, key, callerID(ip))
	pipe.Expire(ctx, key, t.ttl)
	count := pipe.PFCount(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, false
	}
	n := count.Val()
	// Only a new caller can cross the threshold, so the flag write is
	// skipped on the hot path of repeat callers.
	if added.Val() == 1 && n >= t.threshold {
		crossed, _ = t.rdb.SetNX(ctx, key+":flagged", 1, t.ttl).Result()
	}
	return n, crossed
}

// callerID collapses an IPv6 address to its /64: privacy extensions rotate
// the low bits, so one developer would otherwise look like many callers.
func callerID(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}
