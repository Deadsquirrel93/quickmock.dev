package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectCrossSite(t *testing.T) {
	const baseURL = "https://quickmock.dev"

	tests := []struct {
		name         string
		secFetchSite string
		origin       string
		wantStatus   int
		wantCalled   bool
	}{
		{
			name:         "cross-site Sec-Fetch-Site is blocked",
			secFetchSite: "cross-site",
			wantStatus:   http.StatusForbidden,
			wantCalled:   false,
		},
		{
			name:         "same-origin Sec-Fetch-Site passes",
			secFetchSite: "same-origin",
			wantStatus:   http.StatusOK,
			wantCalled:   true,
		},
		{
			name:         "same-site Sec-Fetch-Site passes",
			secFetchSite: "same-site",
			wantStatus:   http.StatusOK,
			wantCalled:   true,
		},
		{
			name:         "none Sec-Fetch-Site passes",
			secFetchSite: "none",
			wantStatus:   http.StatusOK,
			wantCalled:   true,
		},
		{
			name:       "no headers at all passes (curl, CI)",
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "matching Origin passes",
			origin:     baseURL,
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "foreign Origin without Sec-Fetch-Site is blocked",
			origin:     "https://evil.example",
			wantStatus: http.StatusForbidden,
			wantCalled: false,
		},
		{
			name:         "foreign Origin blocked even with same-origin Sec-Fetch-Site",
			secFetchSite: "same-origin",
			origin:       "https://evil.example",
			wantStatus:   http.StatusForbidden,
			wantCalled:   false,
		},
		{
			name:       "trailing slash in baseURL normalizes against Origin without one",
			origin:     baseURL,
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configuredBaseURL := baseURL
			if tt.name == "trailing slash in baseURL normalizes against Origin without one" {
				configuredBaseURL = baseURL + "/"
			}

			called := false
			h := RejectCrossSite(configuredBaseURL)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if called != tt.wantCalled {
				t.Errorf("handler called = %v, want %v", called, tt.wantCalled)
			}
		})
	}
}
