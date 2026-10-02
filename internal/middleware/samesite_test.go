package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRejectCrossSite(t *testing.T) {
	tests := []struct {
		name         string
		method       string // defaults to POST
		secFetchSite string
		origin       string
		wantCalled   bool
	}{
		{name: "cross-site Sec-Fetch-Site is blocked", secFetchSite: "cross-site"},
		{name: "same-site Sec-Fetch-Site is blocked (a sibling subdomain is not us)", secFetchSite: "same-site"},
		{name: "same-origin Sec-Fetch-Site passes", secFetchSite: "same-origin", wantCalled: true},
		{name: "none Sec-Fetch-Site passes (typed URL, bookmark)", secFetchSite: "none", wantCalled: true},
		{name: "no headers at all passes (curl, CI)", wantCalled: true},
		{name: "Origin matching Host passes", origin: "https://quickmock.dev", wantCalled: true},
		{name: "foreign Origin without Sec-Fetch-Site is blocked", origin: "https://evil.example"},
		{name: "unparseable Origin is blocked", origin: "://evil"},
		{name: "safe methods are never blocked", method: http.MethodGet, secFetchSite: "cross-site", wantCalled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			h := RejectCrossSite()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			method := tt.method
			if method == "" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "https://quickmock.dev/", nil)
			if tt.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if called != tt.wantCalled {
				t.Fatalf("handler called = %v, want %v", called, tt.wantCalled)
			}
			if tt.wantCalled {
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want 200", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"cross_site_blocked"`) {
				t.Errorf("rejection = %d %q, want 403 with cross_site_blocked", rec.Code, rec.Body.String())
			}
		})
	}
}
