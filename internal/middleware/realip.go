// Package middleware holds HTTP middlewares specific to this app.
//
// We deliberately don't pull go-chi/chi/middleware: a handful of tiny,
// readable middlewares written here cost less than a dependency in churn
// and audit surface.
package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// realIPCtxKey is private — use IPFromContext to read.
type realIPCtxKey struct{}

// RealIP rewrites r.RemoteAddr to the client's actual IP, derived from the
// first entry in X-Forwarded-For (when present and the request looks like
// it came through our nginx). The original RemoteAddr is preserved in
// context for future debugging if needed.
//
// We only trust XFF when RemoteAddr is loopback — otherwise anyone can
// spoof it.
func RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		ctx := context.WithValue(r.Context(), realIPCtxKey{}, ip)
		r.RemoteAddr = ip
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isLoopback(host) {
		return host
	}
	// Behind nginx: trust XFF.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if first != "" {
			return first
		}
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	return host
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IPFromContext returns the resolved client IP, or "" if absent.
func IPFromContext(ctx context.Context) string {
	v, _ := ctx.Value(realIPCtxKey{}).(string)
	return v
}
