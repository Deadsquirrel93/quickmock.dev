package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestReadFormInputFlaky(t *testing.T) {
	form := url.Values{
		"method":                {"GET"},
		"response_status":       {"200"},
		"response_delay_ms":     {"100"},
		"response_delay_max_ms": {"2000"},
		"error_rate_pct":        {"25"},
		"error_status":          {"503"},
		"error_body":            {`{"error":"boom"}`},
		"seq_status[]":          {"500", "201"},
		"seq_body[]":            {"first", "second"},
		"seq_headers[]":         {"X-Step: one\nno colon line", ""},
		"ttl":                   {"24h"},
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	in := readFormInput(r)

	if in.ResponseDelayMaxMS != 2000 {
		t.Fatalf("delay max = %d, want 2000", in.ResponseDelayMaxMS)
	}
	if in.ErrorRatePct != 25 || in.ErrorResponse == nil ||
		in.ErrorResponse.Status != 503 || in.ErrorResponse.Body != `{"error":"boom"}` {
		t.Fatalf("error config lost: pct=%d resp=%+v", in.ErrorRatePct, in.ErrorResponse)
	}
	if len(in.SequenceSteps) != 2 {
		t.Fatalf("steps = %d, want 2", len(in.SequenceSteps))
	}
	if in.SequenceSteps[0].Status != 500 || in.SequenceSteps[0].Body != "first" {
		t.Fatalf("step 1 wrong: %+v", in.SequenceSteps[0])
	}
	if in.SequenceSteps[0].Headers["X-Step"] != "one" {
		t.Fatalf("step header lost: %+v", in.SequenceSteps[0].Headers)
	}
	if in.SequenceSteps[1].Headers != nil {
		t.Fatalf("empty header textarea must give nil map, got %+v", in.SequenceSteps[1].Headers)
	}
}

func TestReadFormInputNoErrorResponseWhenRateZero(t *testing.T) {
	form := url.Values{
		"method":       {"GET"},
		"error_status": {"503"},
		"error_body":   {"boom"},
		"ttl":          {"24h"},
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	in := readFormInput(r)
	if in.ErrorResponse != nil {
		t.Fatalf("rate 0 must not build an error response, got %+v", in.ErrorResponse)
	}
}

func TestReadFormInputSkipsEmptyStepRows(t *testing.T) {
	form := url.Values{
		"method":        {"GET"},
		"seq_status[]":  {"", "500"},
		"seq_body[]":    {"", "x"},
		"seq_headers[]": {"", ""},
		"ttl":           {"24h"},
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	in := readFormInput(r)
	if len(in.SequenceSteps) != 1 || in.SequenceSteps[0].Status != 500 {
		t.Fatalf("want 1 non-empty step, got %+v", in.SequenceSteps)
	}
}

func TestParseHeaderLines(t *testing.T) {
	got := parseHeaderLines("X-A: 1\r\nX-B:two\nbroken\n: novalue\n")
	if got["X-A"] != "1" || got["X-B"] != "two" {
		t.Fatalf("parse failed: %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("junk lines must be skipped: %v", got)
	}
	if parseHeaderLines("  \n") != nil {
		t.Fatal("blank input must give nil")
	}
}

func TestReadFormInputCORS(t *testing.T) {
	on := url.Values{"method": {"GET"}, "ttl": {"24h"}, "cors_enabled": {"on"}}
	r := httptest.NewRequest("POST", "/", strings.NewReader(on.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if !readFormInput(r).CORSEnabled {
		t.Fatal("cors_enabled=on must set CORSEnabled")
	}

	off := url.Values{"method": {"GET"}, "ttl": {"24h"}}
	r2 := httptest.NewRequest("POST", "/", strings.NewReader(off.Encode()))
	r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r2.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if readFormInput(r2).CORSEnabled {
		t.Fatal("absent checkbox must leave CORSEnabled false")
	}
}

func TestCreateRequestToInputCORS(t *testing.T) {
	req := createMockRequest{Method: "GET", CORSEnabled: true}
	if !req.toInput().CORSEnabled {
		t.Fatal("JSON cors_enabled must reach MockInput")
	}
}

// TestMockRedirectLocation exercises the Post/Redirect/Get target CreateForm
// hands to http.Redirect. It's the load-bearing check for Step 1: the token
// must ride in the URL fragment (never a query param — those reach access
// logs), and legacy mocks without a token must get a plain redirect.
func TestMockRedirectLocation(t *testing.T) {
	withToken := &model.Mock{Slug: "abc123", AdminToken: "qm_" + strings.Repeat("a", 64)}
	loc := mockRedirectLocation(withToken)
	if !strings.HasPrefix(loc, "/mock/abc123") {
		t.Fatalf("location = %q, want prefix /mock/abc123", loc)
	}
	if !strings.Contains(loc, "#token=qm_") {
		t.Fatalf("location = %q, want a #token=qm_ fragment", loc)
	}

	legacy := &model.Mock{Slug: "legacy1"}
	if got := mockRedirectLocation(legacy); got != "/mock/legacy1" {
		t.Fatalf("location = %q, want /mock/legacy1 with no fragment", got)
	}
}

func TestLogsPartialMethod(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty means no filter", "", ""},
		{"valid method uppercased", "post", "POST"},
		{"valid method already upper", "GET", "GET"},
		{"surrounding whitespace trimmed", "  put  ", "PUT"},
		{"garbage is ignored, not rejected", "bogus", ""},
		{"ANY is a mock's match rule, never a request method", "ANY", ""},
		{"ANY lowercase is ignored too", "any", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := logsPartialMethod(c.raw); got != c.want {
				t.Fatalf("logsPartialMethod(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestLogsPartialRendersFilteredMarkup exercises the "partials_logs"
// template directly with the data LogsPartial builds for a ?method=POST
// request. LogsPartial itself needs a live *service.MockService and
// *repository.LogRepo (see log_export_test.go's note on testUI's nil
// svc/logs) — the SQL-level filtering is already covered by
// TestLogRepoListByMockIDMethodFilter, so here the input Logs slice stands
// in for what that already-filtered query would return: only POST rows.
// The rendered markup must carry no GET row, keep the POST option selected,
// and point "download JSON" at the same filter that's on screen.
func TestLogsPartialRendersFilteredMarkup(t *testing.T) {
	u := testUI(t)
	data := map[string]any{
		"Mock": &model.Mock{Slug: "abc123"},
		"Logs": []model.RequestLog{
			{ID: "1", RequestMethod: "POST", RequestIP: "127.0.0.1", CreatedAt: time.Now()},
		},
		"Method":        "POST",
		"FilterMethods": logFilterMethods,
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mock/abc123/logs?method=POST", nil)
	u.renderer.Render(w, req, "partials_logs", http.StatusOK, data)

	body := w.Body.String()
	if strings.Contains(body, "badge-GET") {
		t.Fatalf("filtered markup must not contain a GET row: %s", body)
	}
	if !strings.Contains(body, "badge-POST") {
		t.Fatalf("filtered markup missing the POST row: %s", body)
	}
	if !strings.Contains(body, `<option value="POST" selected>`) {
		t.Fatalf("POST option must be marked selected: %s", body)
	}
	if !strings.Contains(body, "/mock/abc123/logs/export?method=POST") {
		t.Fatalf("download link must carry the same method filter: %s", body)
	}
	if !strings.Contains(body, `id="log-filter-method"`) {
		t.Fatalf("filter select missing: %s", body)
	}
}

func TestBySlugViewOmitsAdminToken(t *testing.T) {
	u := &UI{baseURL: "https://example.test"}
	m := &model.Mock{
		Slug:           "abc123",
		AdminToken:     "qm_" + strings.Repeat("a", 64),
		AdminTokenHash: strings.Repeat("b", 64),
	}
	v := u.bySlugView(m)
	if _, ok := v["admin_token"]; ok {
		t.Fatal("by-slugs view must not include admin_token")
	}
	if _, ok := v["admin_token_hash"]; ok {
		t.Fatal("by-slugs view must not include admin_token_hash")
	}
}
