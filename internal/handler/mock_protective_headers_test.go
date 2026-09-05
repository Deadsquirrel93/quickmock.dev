package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// TestSetMockProtectiveHeaders pins the server-owned headers on /m/:slug.
// MockRouter itself needs a live Postgres to construct, so the header block
// is factored out and asserted here — otherwise this defence has no
// automated coverage at all.
func TestSetMockProtectiveHeaders(t *testing.T) {
	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Content-Security-Policy":      "sandbox; default-src 'none'; frame-ancestors 'none'; base-uri 'none'",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Resource-Policy": "same-origin",
	}

	rec := httptest.NewRecorder()
	setMockProtectiveHeaders(rec)

	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

// TestProtectiveHeadersAreReserved is the other half of the guarantee: the
// serve path writes these last, but a mock author must not be able to set
// them at all. Every name above has to be refused at create/update time.
func TestProtectiveHeadersAreReserved(t *testing.T) {
	for _, name := range []string{
		"X-Content-Type-Options",
		"Content-Security-Policy",
		"X-Frame-Options",
		"Referrer-Policy",
		"Cross-Origin-Resource-Policy",
	} {
		if !service.IsReservedResponseHeader(name) {
			t.Errorf("%s is not reserved — a mock could override it", name)
		}
	}
}
