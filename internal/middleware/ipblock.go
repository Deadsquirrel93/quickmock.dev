package middleware

import (
	"net/http"
	"net/netip"
	"slices"
)

// IPBlocklist rejects requests whose client IP falls in any of prefixes.
// RealIP must run before it so deployments behind a trusted reverse proxy
// match the actual client instead of the proxy hop.
func IPBlocklist(prefixes []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if addr, err := netip.ParseAddr(r.RemoteAddr); err == nil &&
				slices.ContainsFunc(prefixes, func(p netip.Prefix) bool { return p.Contains(addr) }) {
				http.Error(w, `{"error":{"code":"forbidden","message":"Forbidden"}}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
