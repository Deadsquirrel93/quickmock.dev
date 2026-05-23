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
// We trust XFF/X-Real-IP only when the immediate peer is loopback or a
// private/link-local address — i.e. when the request demonstrably came
// from our own infra (the docker bridge or the host loopback in front of
// nginx). For any other peer we ignore the header so external callers
// can't spoof their IP.
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
	if !isTrustedProxy(host) {
		return host
	}
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

// isTrustedProxy reports whether host is an address we control — loopback,
// RFC1918/RFC4193 private, or link-local. Behind docker compose the app
// sees nginx as the bridge gateway (e.g. 172.18.0.1), which is private but
// not loopback.
func isTrustedProxy(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// IPFromContext returns the resolved client IP, or "" if absent.
func IPFromContext(ctx context.Context) string {
	v, _ := ctx.Value(realIPCtxKey{}).(string)
	return v
}
