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
	"net/netip"
	"strings"
)

// realIPCtxKey is private — use IPFromContext to read.
type realIPCtxKey struct{}

// RealIP builds a middleware that rewrites r.RemoteAddr to the client's
// actual IP, read from header (when set) — never from the classic
// X-Forwarded-For/X-Real-IP headers. Those are append-only lists: a
// reverse proxy that is faithful to the spec *adds* its own hop rather than
// replacing the client-supplied value, so trusting "the first entry" (or
// any entry) means trusting whatever the client put there first. This
// service sits behind Cloudflare, which behaves exactly that way, so the
// only safe header to read is one Cloudflare itself sets and overwrites —
// e.g. CF-Connecting-IP — never one it merely appends to.
//
// header is the single header name to trust; pass "" to disable header
// lookups entirely and always use the TCP peer address (the safe default
// for deployments with no such proxy, or where the operator hasn't
// confirmed the header is rewrite-not-append). The original RemoteAddr is
// preserved in context for future debugging if needed.
//
// Even with header set, it is only trusted when the immediate peer is
// loopback or a private/link-local address — i.e. when the request
// demonstrably came from our own infra (the docker bridge or the host
// loopback in front of nginx). For any other peer the header is ignored so
// external callers can't spoof their IP by talking to the app directly.
func RealIP(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, header)
			ctx := context.WithValue(r.Context(), realIPCtxKey{}, ip)
			r.RemoteAddr = ip
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func clientIP(r *http.Request, header string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if header == "" {
		return host
	}
	if !isTrustedProxy(host) {
		return host
	}
	v := strings.TrimSpace(r.Header.Get(header))
	if v == "" {
		return host
	}
	addr, err := netip.ParseAddr(v)
	if err != nil {
		return host
	}
	return addr.String()
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
