package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The SSE request inspector flushes each event. AccessLog wraps the
// ResponseWriter in statusRecorder; if that wrapper hides the base writer's
// Flusher, streaming breaks behind the middleware (observed in production as
// "streaming unsupported"). This guards the Unwrap escape hatch.
func TestAccessLogWriterStaysFlushable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var flushErr error
	h := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flushErr = http.NewResponseController(w).Flush()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if flushErr != nil {
		t.Fatalf("Flush through AccessLog wrapper failed: %v", flushErr)
	}
}
