package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
)

// streamLifetime caps one SSE connection. EventSource reconnects
// automatically, so periodic closes cost nothing and stop zombie
// connections from squatting limiter slots forever.
const streamLifetime = 5 * time.Minute

// flusher walks the ResponseWriter's Unwrap chain to find the underlying
// http.Flusher. Middleware (e.g. the access-log statusRecorder) wraps the
// writer, so a plain w.(http.Flusher) assertion fails behind the middleware
// stack even though the base net/http writer can flush.
func flusher(w http.ResponseWriter) (http.Flusher, bool) {
	for {
		if f, ok := w.(http.Flusher); ok {
			return f, true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil, false
		}
		w = u.Unwrap()
	}
}

// LogsStream handles GET /mock/:slug/logs/stream — a Server-Sent Events
// stream that emits an empty "log" event whenever a new request hits the
// mock. The client reacts by refetching the logs partial; no payload here.
func (u *UI) LogsStream(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !u.inspectorAuthorized(r, m) {
		http.Error(w, "private inspector", http.StatusUnauthorized)
		return
	}
	fl, ok := flusher(w)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ip := mockmw.IPFromContext(r.Context())
	if !u.streams.Acquire(ip) {
		// Client-side JS treats a dead stream as "fall back to polling".
		http.Error(w, "too many streams", http.StatusTooManyRequests)
		return
	}
	defer u.streams.Release(ip)

	// The server's WriteTimeout (90s) would cut long streams; lift it for
	// this response only. Heartbeats below keep proxies from timing out.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // defeat nginx buffering
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()

	events, cancel := u.broker.Subscribe(m.ID)
	defer cancel()
	ping := time.NewTicker(25 * time.Second) // < nginx proxy_read_timeout 60s
	defer ping.Stop()
	lifetime := time.NewTimer(streamLifetime)
	defer lifetime.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-lifetime.C:
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-events:
			fmt.Fprint(w, "event: log\ndata: 1\n\n")
			fl.Flush()
		}
	}
}
