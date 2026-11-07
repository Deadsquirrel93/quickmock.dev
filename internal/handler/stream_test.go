package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
	"github.com/Deadsquirrel93/quickmock.dev/internal/sse"
)

// unwrapOnly mimics a middleware wrapper (like the access-log statusRecorder)
// that captures the writer but does NOT itself implement http.Flusher — it
// only exposes the base writer via Unwrap.
type unwrapOnly struct{ http.ResponseWriter }

func (u unwrapOnly) Unwrap() http.ResponseWriter { return u.ResponseWriter }

// opaque wraps a writer without Flush and without Unwrap — a genuinely
// unflushable chain.
type opaque struct{ http.ResponseWriter }

// LogsStream flushes via http.ResponseController, which must reach the base
// writer through middleware wrappers that only expose Unwrap.
func TestResponseControllerFlushesThroughUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := http.NewResponseController(unwrapOnly{unwrapOnly{rec}}).Flush(); err != nil || !rec.Flushed {
		t.Fatalf("Flush through Unwrap chain: err=%v flushed=%v", err, rec.Flushed)
	}
	if err := http.NewResponseController(opaque{httptest.NewRecorder()}).Flush(); err == nil {
		t.Fatal("want an error when no flusher is in the chain")
	}
}

// On shutdown the server waits for open connections; a live inspector
// stream must end when the stop signal fires instead of holding the deploy
// up for its whole five-minute lifetime.
func TestLogsStreamEndsOnStop(t *testing.T) {
	store := &fakeCreateStore{created: []*model.Mock{{ID: "m1", Slug: "live", LogsPublic: true}}}
	u := creatingUI(t, store, 50)
	u.broker = sse.NewBroker()
	u.streams = sse.NewStreamLimiter(10, 10)
	stop := make(chan struct{})
	u.stop = stop

	done := make(chan struct{})
	go func() {
		u.LogsStream(httptest.NewRecorder(), withSlug(httptest.NewRequest("GET", "/mock/live/logs/stream", nil), "live"))
		close(done)
	}()
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream kept running after the stop signal")
	}
}

// The private inspector authorizes against the mock it already loaded:
// re-reading it from the store doubled the queries of every poll.
func TestPrivateInspectorLoadsMockOnce(t *testing.T) {
	plain, hash, err := service.GenerateAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeCreateStore{created: []*model.Mock{{ID: "m1", Slug: "priv", AdminTokenHash: hash}}}
	u := creatingUI(t, store, 50)
	u.broker = sse.NewBroker()
	u.streams = sse.NewStreamLimiter(10, 10)
	stop := make(chan struct{})
	close(stop)
	u.stop = stop

	req := withSlug(httptest.NewRequest("GET", "/mock/priv/logs/stream", nil), "priv")
	req.Header.Set("Authorization", "Bearer "+plain)
	w := httptest.NewRecorder()
	u.LogsStream(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 for the right token", w.Code)
	}
	if store.reads != 1 {
		t.Fatalf("mock read %d times, want 1", store.reads)
	}
}

func TestPrivateInspectorRejectsWrongToken(t *testing.T) {
	_, hash, err := service.GenerateAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeCreateStore{created: []*model.Mock{{ID: "m1", Slug: "priv", AdminTokenHash: hash}}}
	u := creatingUI(t, store, 50)
	req := withSlug(httptest.NewRequest("GET", "/mock/priv/logs/stream", nil), "priv")
	req.Header.Set("Authorization", "Bearer qm_wrong")
	w := httptest.NewRecorder()
	u.LogsStream(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", w.Code)
	}
}
