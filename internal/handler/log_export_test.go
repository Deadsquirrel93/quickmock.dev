package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// These tests cover everything about the log export handler that doesn't
// require a live database: query-parameter validation, the
// service-error-to-HTTP-status mapping, and the download filename. The
// authorization + listing flow itself (a Bearer token against
// MockService.AuthorizeSlug, then LogRepo.ListByMockID) needs a real
// Postgres-backed *service.MockService/*repository.LogRepo pair, which
// internal/handler has no test harness for (see testAPI/testUI: both build
// their struct with a nil svc/logs). That end-to-end path — 401 without a
// header, 403 on a wrong token, 200 with a JSON body and
// Content-Disposition header, and ?method=POST narrowing the rows — is
// covered by manual curl verification instead.

func TestLogMethodFilter(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{"empty means no filter", "", "", true},
		{"valid method uppercased", "post", "POST", true},
		{"valid method already upper", "GET", "GET", true},
		{"surrounding whitespace trimmed", "  put  ", "PUT", true},
		{"garbage rejected", "bogus", "", false},
		{"empty after trim rejected as not-empty garbage is fine", "   ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := logMethodFilter(c.raw)
			if ok != c.wantOK {
				t.Fatalf("logMethodFilter(%q) ok = %v, want %v", c.raw, ok, c.wantOK)
			}
			if got != c.want {
				t.Fatalf("logMethodFilter(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestLogsExportFilename(t *testing.T) {
	got := logsExportFilename("abc123XYZ")
	if got != "quickmock-abc123XYZ-logs.json" {
		t.Fatalf("logsExportFilename = %q", got)
	}
}

func TestWriteLogsExportErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", service.ErrNotFound, http.StatusNotFound, "not_found"},
		{"token required", service.ErrTokenRequired, http.StatusUnauthorized, "admin_token_required"},
		{"token invalid", service.ErrTokenInvalid, http.StatusForbidden, "admin_token_invalid"},
	}
	u := testUI(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/mock/abc123/logs/export", nil)
			u.writeLogsExportError(w, r, c.err)
			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, c.wantStatus)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v (%s)", err, w.Body.String())
			}
			if body.Error.Code != c.wantCode {
				t.Fatalf("error code = %q, want %q", body.Error.Code, c.wantCode)
			}
		})
	}
}
