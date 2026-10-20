package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

const manyCallersMsg = "mock has many distinct callers"

// callerRouter is servingRouter with a live CallerTracker and a logger the
// test can read.
func callerRouter(t *testing.T, m *model.Mock, threshold int64) (*MockRouter, *bytes.Buffer) {
	t.Helper()
	addr := os.Getenv("QUICKMOCK_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("QUICKMOCK_TEST_REDIS_ADDR not set, skipping test that needs Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis at %s: %v", addr, err)
	}
	t.Cleanup(func() { rdb.Close() })

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	store := &fakeCreateStore{created: []*model.Mock{m}}
	svc := service.NewMockService(store, nil, nil, 512*1024, 50, 168*time.Hour, 720*time.Hour, nil)
	logs := service.NewLogWriter(nil, nil, nil, 16, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	return NewMockRouter(svc, logs, nil, repository.NewCallerTracker(rdb, threshold), logger, ""), &buf
}

func callFrom(h *MockRouter, m *model.Mock, method, ip string) int {
	r := withSlug(httptest.NewRequest(method, "/m/"+m.Slug, nil), m.Slug)
	r.RemoteAddr = ip // RealIP has already stripped the port in production
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func callerTestMock(t *testing.T) *model.Mock {
	expires := time.Now().Add(time.Hour)
	return &model.Mock{
		// Unique per run: the tracker keys on the mock ID in a shared Redis.
		ID:             t.Name() + time.Now().Format("150405.000000000"),
		Slug:           "callers1",
		Method:         model.MethodGET,
		ResponseStatus: 200,
		ResponseBody:   `{"ok":true}`,
		ExpiresAt:      &expires,
		CreatorIP:      "198.51.100.77",
	}
}

func TestServeWarnsOnceWhenMockReachesCallerThreshold(t *testing.T) {
	m := callerTestMock(t)
	h, logs := callerRouter(t, m, 3)

	for _, ip := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.1"} {
		if code := callFrom(h, m, http.MethodGet, ip); code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
	}
	if strings.Contains(logs.String(), manyCallersMsg) {
		t.Fatalf("warned below threshold:\n%s", logs.String())
	}

	for _, ip := range []string{"203.0.113.3", "203.0.113.4", "203.0.113.5"} {
		if code := callFrom(h, m, http.MethodGet, ip); code != http.StatusOK {
			t.Fatalf("status = %d, want 200: the signal must not change the response", code)
		}
	}
	out := logs.String()
	if got := strings.Count(out, manyCallersMsg); got != 1 {
		t.Fatalf("warnings = %d, want exactly 1:\n%s", got, out)
	}
	for _, want := range []string{"level=WARN", "slug=callers1", "creator_ip=198.51.100.77", "callers_today=3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("warning lacks %q:\n%s", want, out)
		}
	}
}

// A request that never reaches a mock's response (wrong method) is still
// a caller of that mock: abuse can probe with any method.
func TestServeCountsCallersOnMethodMismatch(t *testing.T) {
	m := callerTestMock(t)
	h, logs := callerRouter(t, m, 2)

	callFrom(h, m, http.MethodGet, "203.0.113.1")
	if code := callFrom(h, m, http.MethodPost, "203.0.113.2"); code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", code)
	}
	if !strings.Contains(logs.String(), manyCallersMsg) {
		t.Fatalf("405 caller not counted:\n%s", logs.String())
	}
}

// Unknown slugs are noise from scanners; they must not create Redis keys.
func TestServeDoesNotTrackUnknownSlug(t *testing.T) {
	m := callerTestMock(t)
	h, logs := callerRouter(t, m, 1)

	r := withSlug(httptest.NewRequest(http.MethodGet, "/m/nope", nil), "nope")
	r.RemoteAddr = "203.0.113.1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if strings.Contains(logs.String(), manyCallersMsg) {
		t.Fatalf("unknown slug was tracked:\n%s", logs.String())
	}
}
