package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// Stat keys used across the codebase. Constants keep us from typo-bumping
// the wrong counter and silently writing to a new row.
const (
	StatMocksCreated   = "mocks_created"
	StatRequestsServed = "requests_served"
)

// StatsCache wraps StatsRepo with a process-local cache. The home page
// reads the snapshot on every render — without the cache that's one DB
// round-trip per home page view. With the cache it's amortised to one
// query per `ttl` per process.
type StatsCache struct {
	repo *repository.StatsRepo
	ttl  time.Duration
	log  *slog.Logger

	mu       sync.RWMutex
	snapshot map[string]int64
	loadedAt time.Time
}

func NewStatsCache(repo *repository.StatsRepo, ttl time.Duration, log *slog.Logger) *StatsCache {
	return &StatsCache{repo: repo, ttl: ttl, log: log, snapshot: map[string]int64{}}
}

// Snapshot returns the current view of all counters. If the cache is fresh
// enough, returns the cached map; otherwise queries the DB and refreshes.
// Errors are logged and the stale snapshot is returned — never blocking
// the home page on a transient DB hiccup.
func (c *StatsCache) Snapshot(ctx context.Context) map[string]int64 {
	c.mu.RLock()
	if time.Since(c.loadedAt) < c.ttl {
		out := c.snapshot
		c.mu.RUnlock()
		return out
	}
	c.mu.RUnlock()

	fresh, err := c.repo.All(ctx)
	if err != nil {
		c.log.Warn("stats refresh failed", slog.Any("err", err))
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.snapshot
	}
	c.mu.Lock()
	c.snapshot = fresh
	c.loadedAt = time.Now()
	c.mu.Unlock()
	return fresh
}

// BumpAsync increments key by n in a goroutine, never blocking the caller.
// Errors are logged. Use this on hot paths (mock creation, request hit).
func (c *StatsCache) BumpAsync(key string, n int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.repo.Bump(ctx, key, n); err != nil {
			c.log.Warn("stats bump failed",
				slog.String("key", key),
				slog.Int64("n", n),
				slog.Any("err", err))
		}
	}()
}
