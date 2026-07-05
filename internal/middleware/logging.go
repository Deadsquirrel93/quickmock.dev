package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder wraps ResponseWriter to capture the response code so we
// can log it. http.Hijacker / Pusher are deliberately not implemented —
// this app doesn't use them.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap exposes the underlying ResponseWriter. Without it, wrapping this
// recorder hides the base writer's http.Flusher and deadline setters from
// http.NewResponseController and from streaming handlers — which breaks the
// SSE request inspector (it needs to Flush each event).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// AccessLog emits one structured log line per request.
//
// Quiet by default for /healthz to keep journals readable.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if r.URL.Path == "/healthz" {
				return
			}
			logger.Info("http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("dur", time.Since(start)),
				slog.String("ip", IPFromContext(r.Context())),
			)
		})
	}
}
