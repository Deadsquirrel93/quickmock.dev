package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
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
