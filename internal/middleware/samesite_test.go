package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectCrossSite(t *testing.T) {
	const baseURL = "https://quickmock.dev"

	tests := []struct {
		name         string
		baseURL      string // defaults to baseURL when empty
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
			baseURL:    baseURL + "/",
			origin:     baseURL,
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "host letter-case does not decide the outcome",
			baseURL:    "https://QuickMock.DEV",
			origin:     "https://quickmock.dev",
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "unparseable Origin is treated as a mismatch",
			origin:     "://evil",
			wantStatus: http.StatusForbidden,
			wantCalled: false,
		},
		{
			name:       "unusable base URL rejects requests carrying an Origin",
			baseURL:    "://broken",
			origin:     baseURL,
			wantStatus: http.StatusForbidden,
			wantCalled: false,
		},
		{
			name:       "unusable base URL still lets header-less clients through",
			baseURL:    "://broken",
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configuredBaseURL := tt.baseURL
			if configuredBaseURL == "" {
				configuredBaseURL = baseURL
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))

			called := false
			h := RejectCrossSite(configuredBaseURL, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
