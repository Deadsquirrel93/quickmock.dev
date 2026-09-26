package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInspectorPollStopsWhenMockIsGone covers the two inspector endpoints
// the mock page polls. htmx does not swap a 404, so a deleted or expired
// mock used to leave an open tab re-requesting /logs and /summary every few
// seconds for days. For an htmx request the handlers now answer 286 — htmx's
// "stop polling" status — with a fragment that carries no hx-trigger of its
// own, so the swap cannot restart the loop. Plain requests keep the 404.
func TestInspectorPollStopsWhenMockIsGone(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{}, 50)

	for _, tc := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"logs", "/mock/gone12345678/logs", u.LogsPartial},
		{"summary", "/mock/gone12345678/summary", u.SummaryPartial},
	} {
		t.Run(tc.name+" htmx", func(t *testing.T) {
			req := withSlug(httptest.NewRequest(http.MethodGet, tc.path, nil), "gone12345678")
			req.Header.Set("HX-Request", "true")
			w := httptest.NewRecorder()
			tc.handler(w, req)
			if w.Code != statusStopPolling {
				t.Fatalf("status = %d, want %d", w.Code, statusStopPolling)
			}
			body := w.Body.String()
			if strings.Contains(body, "hx-trigger") || strings.Contains(body, "hx-get") {
				t.Fatalf("gone fragment must not re-arm polling: %s", body)
			}
			if !strings.Contains(body, "deleted or has expired") {
				t.Fatalf("gone fragment lacks the explanation: %s", body)
			}
		})

		t.Run(tc.name+" plain", func(t *testing.T) {
			req := withSlug(httptest.NewRequest(http.MethodGet, tc.path, nil), "gone12345678")
			w := httptest.NewRecorder()
			tc.handler(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", w.Code)
			}
		})
	}
}
