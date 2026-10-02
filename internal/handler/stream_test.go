package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
