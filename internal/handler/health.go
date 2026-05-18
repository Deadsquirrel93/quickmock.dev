package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// Health writes "ok" when both Postgres and Redis are reachable, 503 with
// JSON otherwise. Used by load balancers, docker healthchecks, deploy.sh.
func Health(pool *pgxpool.Pool, rl *repository.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unhealthy","cause":"postgres"}`))
			return
		}
		if err := rl.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unhealthy","cause":"redis"}`))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}
}
