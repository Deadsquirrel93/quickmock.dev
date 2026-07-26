package repository

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// testPool connects to QUICKMOCK_PG_DSN (the same env var CI already
// exports for `go test ./...`, see .github/workflows/ci.yml) and applies
// migrations. Tests that need it skip when the DSN isn't set, so `go test`
// stays runnable on a machine without Postgres.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("QUICKMOCK_PG_DSN")
	if dsn == "" {
		t.Skip("QUICKMOCK_PG_DSN not set, skipping test that needs Postgres")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	sub, err := fs.Sub(quickmock.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("migrations fs: %v", err)
	}
	if err := RunMigrations(ctx, pool, sub, "."); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return pool
}

// seedMock inserts a minimal mock row so request_logs (FK to mocks.id) has
// something to point at.
func seedMock(ctx context.Context, t *testing.T, repo *MockRepo, slug string) *model.Mock {
	t.Helper()
	m := &model.Mock{
		Slug:           slug,
		Method:         model.MethodANY,
		ResponseStatus: 200,
		ContentType:    "text/plain",
	}
	if err := repo.Create(ctx, m); err != nil {
		t.Fatalf("seed mock: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.DeleteBySlug(context.Background(), slug)
	})
	return m
}

func TestLogRepoListByMockIDMethodFilter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	mockRepo := NewMockRepo(pool)
	logRepo := NewLogRepo(pool)

	slug := fmt.Sprintf("log-filter-test-%d", time.Now().UnixNano())
	m := seedMock(ctx, t, mockRepo, slug)

	seedMethods := []string{"GET", "POST", "POST", "DELETE"}
	for _, method := range seedMethods {
		if err := logRepo.Insert(ctx, &model.RequestLog{
			MockID:        m.ID,
			RequestMethod: method,
			RequestIP:     "127.0.0.1",
		}); err != nil {
			t.Fatalf("insert log (%s): %v", method, err)
		}
	}

	t.Run("empty filter matches previous unfiltered behavior", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 50, time.Time{}, LogFilter{})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != len(seedMethods) {
			t.Fatalf("got %d logs, want %d", len(logs), len(seedMethods))
		}
	})

	t.Run("method filter narrows to exact matches", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 50, time.Time{}, LogFilter{Method: "POST"})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != 2 {
			t.Fatalf("got %d logs, want 2", len(logs))
		}
		for _, l := range logs {
			if l.RequestMethod != "POST" {
				t.Fatalf("unexpected method %q leaked into POST-filtered results", l.RequestMethod)
			}
		}
	})

	t.Run("method filter combines with limit and since", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 1, time.Time{}, LogFilter{Method: "POST"})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("got %d logs, want 1 (limit not respected alongside filter)", len(logs))
		}
		if logs[0].RequestMethod != "POST" {
			t.Fatalf("unexpected method %q", logs[0].RequestMethod)
		}
	})

	t.Run("method filter matching nothing returns empty slice", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 50, time.Time{}, LogFilter{Method: "PUT"})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != 0 {
			t.Fatalf("got %d logs, want 0", len(logs))
		}
	})
}
