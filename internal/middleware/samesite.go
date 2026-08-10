package middleware

import (
	"net/http"
	"net/url"
)

// RejectCrossSite blocks state-changing requests that a third-party page
// triggered in the user's browser. Non-browser clients send neither header
// and pass through untouched.
func RejectCrossSite(baseURL string) func(http.Handler) http.Handler {
	base, _ := url.Parse(baseURL)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				rejectCrossSite(w)
				return
			}

			if origin := r.Header.Get("Origin"); origin != "" {
				originURL, err := url.Parse(origin)
				if err != nil || base == nil ||
					originURL.Scheme != base.Scheme || originURL.Host != base.Host {
					rejectCrossSite(w)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func rejectCrossSite(w http.ResponseWriter) {
	http.Error(w, `{"error":{"code":"cross_site_blocked","message":"Cross-site request blocked."}}`, http.StatusForbidden)
}
