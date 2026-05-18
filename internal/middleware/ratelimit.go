package middleware

import (
	"net/http"
	"strconv"

	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// RateLimit returns a middleware that consults `limiter` once per request,
// keyed by client IP + the static `bucket` name. On a denied request it
// writes 429 with Retry-After and short-circuits the chain.
func RateLimit(limiter *repository.RateLimiter, bucket string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := IPFromContext(r.Context())
			if ip == "" {
				ip = r.RemoteAddr
			}
			dec, err := limiter.Allow(r.Context(), "rl:"+bucket+":"+ip)
			if err != nil {
				// Fail open — better serve a request than 500 the user.
				next.ServeHTTP(w, r)
				return
			}
			if !dec.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(dec.RetryAfter.Seconds())))
				http.Error(w, `{"error":{"code":"rate_limited","message":"Too many requests."}}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
