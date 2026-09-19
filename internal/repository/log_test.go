package repository

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
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

// TestLogRepoStatusFilter covers the response_status column end to end, and
// in particular the shape migration 008 has to leave behind: the column must
// be NOT NULL DEFAULT 0, because ListByMockID scans it into a plain int and
// pgx errors with "cannot scan NULL into *int" on a NULL. The first cut of
// 008 added the column nullable, which made every log read fail against rows
// written before the deploy — a fresh test database never reproduced it
// because Insert always supplies a value.
func TestLogRepoStatusFilter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	mockRepo := NewMockRepo(pool)
	logRepo := NewLogRepo(pool)

	slug := fmt.Sprintf("log-status-test-%d", time.Now().UnixNano())
	m := seedMock(ctx, t, mockRepo, slug)

	for _, status := range []int{200, 200, 404, 500} {
		if err := logRepo.Insert(ctx, &model.RequestLog{
			MockID:         m.ID,
			RequestMethod:  "GET",
			RequestIP:      "127.0.0.1",
			ResponseStatus: status,
		}); err != nil {
			t.Fatalf("insert log (%d): %v", status, err)
		}
	}

	t.Run("column is not nullable and defaults to 0", func(t *testing.T) {
		var nullable string
		var def *string
		err := pool.QueryRow(ctx, `
			SELECT is_nullable, column_default
			FROM information_schema.columns
			WHERE table_name = 'request_logs' AND column_name = 'response_status'
		`).Scan(&nullable, &def)
		if err != nil {
			t.Fatalf("inspect column: %v", err)
		}
		if nullable != "NO" {
			t.Errorf("response_status is_nullable = %q, want %q — a NULL here breaks every ListByMockID call", nullable, "NO")
		}
		if def == nil || !strings.HasPrefix(*def, "0") {
			t.Errorf("response_status column_default = %v, want 0", def)
		}
	})

	t.Run("status filter narrows to exact matches", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 50, time.Time{}, LogFilter{Status: 200})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != 2 {
			t.Fatalf("got %d logs, want 2", len(logs))
		}
		for _, l := range logs {
			if l.ResponseStatus != 200 {
				t.Fatalf("unexpected status %d leaked into 200-filtered results", l.ResponseStatus)
			}
		}
	})

	t.Run("zero status means no filter", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 50, time.Time{}, LogFilter{Status: 0})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != 4 {
			t.Fatalf("got %d logs, want 4", len(logs))
		}
	})

	t.Run("method and status filters combine", func(t *testing.T) {
		logs, err := logRepo.ListByMockID(ctx, m.ID, 50, time.Time{}, LogFilter{Method: "GET", Status: 500})
		if err != nil {
			t.Fatalf("ListByMockID: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("got %d logs, want 1", len(logs))
		}
	})
}
