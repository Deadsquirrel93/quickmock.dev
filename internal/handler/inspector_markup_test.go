package handler

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// TestLogsPartialCarriesNoInlineScript: htmx runs a swapped-in <script> at
// settle time, and when two logs fetches overlap the first fragment's script
// is already detached — htmx then throws "insertBefore of null". The partial
// must stay script-free; the box's wiring lives on #qm-logs in mock.html.
func TestLogsPartialCarriesNoInlineScript(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.renderer.Render(w, httptest.NewRequest(http.MethodGet, "/mock/abc123/logs", nil), "partials_logs", http.StatusOK, map[string]any{
		"Mock": &model.Mock{Slug: "abc123"}, "Logs": []model.RequestLog{}, "Method": "", "Status": 0,
		"FilterMethods": logFilterMethods, "FilterStatuses": logFilterStatuses,
	})
	body := w.Body.String()
	if !strings.Contains(body, "log-filter-status") || !strings.Contains(body, `class="empty"`) {
		t.Fatalf("partials_logs did not render completely:\n%s", body)
	}
	if strings.Contains(body, "<script") {
		t.Fatalf("partials_logs must not carry a <script>:\n%s", body)
	}
}

func renderMockPage(t *testing.T, locked bool) string {
	t.Helper()
	u := testUI(t)
	w := httptest.NewRecorder()
	m := &model.Mock{Slug: "abc123", Method: model.MethodGET, ResponseStatus: 200, ContentType: "text/plain"}
	u.renderer.Render(w, httptest.NewRequest(http.MethodGet, "/mock/abc123", nil), "mock", http.StatusOK, map[string]any{
		"Mock": m, "URL": "https://example.test/m/abc123", "Methods": model.AllMethods,
		"MaxBodyKB": 512, "InspectorLocked": locked, "Logs": []model.RequestLog{},
	})
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "</html>") {
		t.Fatalf("mock page did not render completely (status %d)", w.Code)
	}
	return body
}

var logsBoxRe = regexp.MustCompile(`(?s)<div id="qm-logs"[^>]*>`)

func TestMockPageLogsBoxWiring(t *testing.T) {
	for _, locked := range []bool{true, false} {
		body := renderMockPage(t, locked)
		box := logsBoxRe.FindString(body)
		if box == "" {
			t.Fatal("#qm-logs box not rendered")
		}
		for _, want := range []string{`hx-include="#qm-logs"`, `hx-sync="this:queue last"`, `hx-trigger="load, qm:log"`} {
			if !strings.Contains(box, want) {
				t.Errorf("locked=%v: #qm-logs missing %s: %s", locked, want, box)
			}
		}
		// The live stream may start at page load only when the server has
		// already authorized this inspector; a locked one waits for the
		// session exchange, or the stream 401s and falls back to polling.
		if got := strings.Contains(box, "data-authorized"); got == locked {
			t.Errorf("locked=%v: data-authorized present = %v: %s", locked, got, box)
		}
	}
}

func TestMockPageClearLogsRefreshesViaQmLog(t *testing.T) {
	body := renderMockPage(t, false)
	if strings.Contains(body, "new Event('htmx:trigger')") {
		t.Error("clearLogs dispatches a DOM event htmx never listens to; trigger qm:log on #qm-logs instead")
	}
}
