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

func TestFlusherReachesWriterThroughUnwrap(t *testing.T) {
	rec := httptest.NewRecorder() // implements http.Flusher
	if _, ok := flusher(unwrapOnly{rec}); !ok {
		t.Fatal("flusher must reach the base writer through the Unwrap chain")
	}
	// Two layers deep still resolves.
	if _, ok := flusher(unwrapOnly{unwrapOnly{rec}}); !ok {
		t.Fatal("flusher must follow nested Unwrap wrappers")
	}
}

func TestFlusherReportsUnsupported(t *testing.T) {
	// opaque neither flushes nor unwraps → genuinely unsupported.
	if _, ok := flusher(opaque{httptest.NewRecorder()}); ok {
		t.Fatal("flusher must report false when no flusher is in the chain")
	}
}
