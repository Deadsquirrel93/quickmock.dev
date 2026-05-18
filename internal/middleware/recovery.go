package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recoverer catches panics from downstream handlers, logs them with stack,
// and returns a generic 500 to the client. It never leaks the panic
// message to the response — the client gets a stable, boring error JSON.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("handler panic",
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
						slog.String("method", r.Method),
						slog.String("stack", string(debug.Stack())),
					)
					http.Error(w,
						`{"error":{"code":"internal_error","message":"Something went wrong."}}`,
						http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
