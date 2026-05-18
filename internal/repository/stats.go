package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StatsRepo is a thin adapter over the `stats` table. The table is a tiny
// key→bigint map; we use it for lifetime counters that must survive mock
// deletion (mocks_created, requests_served).
type StatsRepo struct {
	pool *pgxpool.Pool
}

func NewStatsRepo(pool *pgxpool.Pool) *StatsRepo { return &StatsRepo{pool: pool} }

// Bump atomically adds n (which may be negative for tests) to the counter.
// Creates the row if it doesn't exist — handy when a new counter key ships
// without a migration to seed it.
func (r *StatsRepo) Bump(ctx context.Context, key string, n int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO stats (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = stats.value + EXCLUDED.value
	`, key, n)
	return err
}

// All returns every counter as a map. Cheap query (single sequential scan
// of a tiny table), but the home-page handler still wraps it in a 30s
// process-local cache to avoid 1 query per page view.
func (r *StatsRepo) All(ctx context.Context) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT key, value FROM stats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64, 4)
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
