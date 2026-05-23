package middleware

import "net/http"

// SecurityHeaders sets the universal hardening headers that apply to every
// response: clickjacking + MIME-sniffing protection, a conservative referrer
// policy, and (when secure=true, i.e. when the site is served over TLS) HSTS.
//
// These are independent of any per-route CSP — UI/API and the mock router
// own their own CSP because the trust model differs (UI resources are
// first-party; mock responses are arbitrary user content).
func SecurityHeaders(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "interest-cohort=()")
			if secure {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UICSP applies the Content-Security-Policy that fits the first-party UI.
//
// 'unsafe-inline' + 'unsafe-eval' are required by Alpine.js (it builds
// reactive expressions via `new Function()`) and by the small inline
// scripts in base.html (theme bootstrap, component definitions). Moving to
// a nonce-based CSP would let us drop both — left as a future hardening.
//
// frame-ancestors 'none' duplicates X-Frame-Options for browsers that
// honor only one. form-action 'self' kills cross-origin form posts.
func UICSP() func(http.Handler) http.Handler {
	const policy = "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
		"style-src 'self'; " +
		"img-src 'self' data:; " +
		"font-src 'self'; " +
		"connect-src 'self'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"object-src 'none'"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", policy)
			next.ServeHTTP(w, r)
		})
	}
}
