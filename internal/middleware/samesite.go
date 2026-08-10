package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// RejectCrossSite blocks state-changing requests that a third-party page
// triggered in the user's browser. Non-browser clients (curl, CI, the public
// API's own callers) send neither Sec-Fetch-Site nor Origin, so they pass
// through untouched: this guards the browser path only, deliberately leaving
// scripted access working.
//
// An Origin that is present but does not match is rejected even when
// Sec-Fetch-Site says same-origin — a header a page can influence never
// upgrades a mismatching Origin.
func RejectCrossSite(baseURL string, logger *slog.Logger) func(http.Handler) http.Handler {
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" {
		// Fail closed for requests carrying an Origin, but say so at startup:
		// otherwise a malformed base URL reads as an unexplained outage of
		// every browser-initiated create.
		logger.Error("cross-site guard: unusable base URL, browser requests carrying Origin will be rejected",
			"base_url", baseURL, "err", err)
		base = nil
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				rejectCrossSite(w)
				return
			}

			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(base, origin) {
				rejectCrossSite(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// sameOrigin reports whether origin names the same scheme and host as base.
// Both comparisons are case-insensitive: url.Parse lowercases the scheme but
// leaves the host exactly as written, and the configured base URL is never
// canonicalized. An unparseable Origin counts as a mismatch — a header that
// is present but malformed is not the same as no header at all.
func sameOrigin(base *url.URL, origin string) bool {
	if base == nil {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, base.Scheme) && strings.EqualFold(u.Host, base.Host)
}

func rejectCrossSite(w http.ResponseWriter) {
	http.Error(w, `{"error":{"code":"cross_site_blocked","message":"Cross-site request blocked."}}`, http.StatusForbidden)
}
