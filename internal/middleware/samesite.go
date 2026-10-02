package middleware

import "net/http"

// RejectCrossSite blocks state-changing requests that a third-party page
// triggered in the user's browser, using net/http's CrossOriginProtection:
// Sec-Fetch-Site must be same-origin or none, or — for browsers that omit
// it — Origin must match the Host header. Non-browser clients (curl, CI, the
// public API's own callers) send neither header, so they pass through
// untouched: this guards the browser path only.
func RejectCrossSite() func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"cross_site_blocked","message":"Cross-site request blocked."}}`, http.StatusForbidden)
	}))
	return cop.Handler
}
