package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// renderedPrefill extracts and decodes the window.__qmPrefill object the
// home page hands to the form's applyConfig.
func renderedPrefill(t *testing.T, page string) map[string]any {
	t.Helper()
	const marker = "window.__qmPrefill = "
	i := strings.Index(page, marker)
	if i < 0 {
		t.Fatalf("page has no %q: the form would come back empty", marker)
	}
	rest := page[i+len(marker):]
	end := strings.Index(rest, ";</script>")
	if end < 0 {
		end = strings.Index(rest, ";\n")
	}
	if end < 0 {
		t.Fatal("cannot find the end of the prefill statement")
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(rest[:end]), &p); err != nil {
		t.Fatalf("prefill is not a JSON object: %v\n%s", err, rest[:end])
	}
	return p
}

// fullForm is a submission touching every field family applyConfig restores.
func fullForm(slug string) url.Values {
	return url.Values{
		"slug":                  {slug},
		"name":                  {"orders api"},
		"method":                {"POST"},
		"response_status":       {"201"},
		"response_body":         {`{"id":42}`},
		"content_type":          {"application/json"},
		"header_name[]":         {"X-Demo"},
		"header_value[]":        {"yes"},
		"response_delay_ms":     {"100"},
		"response_delay_max_ms": {"300"},
		"error_rate_pct":        {"10"},
		"error_status":          {"503"},
		"error_body":            {"down"},
		"seq_status[]":          {"500"},
		"seq_body[]":            {"again"},
		"seq_headers[]":         {"X-Try: 2"},
		"path_suffix":           {"orders/42"},
		"cors_enabled":          {"on"},
		"logs_public":           {"on"},
		"ttl":                   {"720h"},
	}
}

func assertPrefillCarries(t *testing.T, p map[string]any, slug string) {
	t.Helper()
	want := map[string]any{
		"slug":                  slug,
		"name":                  "orders api",
		"method":                "POST",
		"response_status":       float64(201),
		"response_body":         `{"id":42}`,
		"content_type":          "application/json",
		"response_delay_ms":     float64(100),
		"response_delay_max_ms": float64(300),
		"error_rate_pct":        float64(10),
		"path_suffix":           "orders/42",
		"cors_enabled":          true,
		"logs_public":           true,
		"capture_body":          false, // unchecked boxes are absent from the form
		"capture_ip":            false,
		"ttl":                   "720h",
	}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("prefill[%q] = %#v, want %#v", k, p[k], v)
		}
	}
	if h, _ := p["response_headers"].(map[string]any); h["X-Demo"] != "yes" {
		t.Errorf("prefill response_headers = %#v", p["response_headers"])
	}
	if e, _ := p["error_response"].(map[string]any); e["status"] != float64(503) || e["body"] != "down" {
		t.Errorf("prefill error_response = %#v", p["error_response"])
	}
	seq, _ := p["response_sequence"].([]any)
	if len(seq) != 1 {
		t.Fatalf("prefill response_sequence = %#v, want 1 step", p["response_sequence"])
	}
	if s := seq[0].(map[string]any); s["status"] != float64(500) || s["body"] != "again" {
		t.Errorf("prefill sequence step = %#v", s)
	}
}

func TestCreateFormSlugTakenKeepsInput(t *testing.T) {
	store := &fakeCreateStore{created: []*model.Mock{{Slug: "taken-one"}}}
	u := creatingUI(t, store, 50)

	w := postCreateForm(u, fullForm("taken-one"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 re-render", w.Code)
	}
	if !strings.Contains(w.Body.String(), "This slug is already taken") {
		t.Fatal("slug_taken banner missing")
	}
	assertPrefillCarries(t, renderedPrefill(t, w.Body.String()), "taken-one")
}

func TestCreateFormPerIPLimitKeepsInput(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{perIPUsed: 50}, 50)
	w := postCreateForm(u, fullForm("fresh-slug"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 re-render", w.Code)
	}
	assertPrefillCarries(t, renderedPrefill(t, w.Body.String()), "fresh-slug")
}

// readFormInputChecked rejects malformed routes JSON before the service runs.
func TestCreateFormParseErrorKeepsInput(t *testing.T) {
	store := &fakeCreateStore{}
	u := creatingUI(t, store, 50)
	form := fullForm("fresh-slug")
	form.Set("routes_json", "{not json")

	w := postCreateForm(u, form)
	if w.Code != http.StatusOK || len(store.created) != 0 {
		t.Fatalf("status = %d, created = %d; want 200 re-render and nothing stored", w.Code, len(store.created))
	}
	p := renderedPrefill(t, w.Body.String())
	assertPrefillCarries(t, p, "fresh-slug")
	// The broken JSON is what the user must fix, so it comes back verbatim.
	if p["routes_json"] != "{not json" {
		t.Errorf("prefill routes_json = %#v, want the raw text", p["routes_json"])
	}
}

// JSON textareas come back as typed (formatting included), not re-encoded.
func TestCreateFormKeepsRawJSONTextareas(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{perIPUsed: 50}, 50)
	form := fullForm("fresh-slug")
	form.Set("response_variants_json", "[\n  {\"name\": \"v1\", \"status\": 202}\n]")
	form.Set("response_rules_json", "")
	p := renderedPrefill(t, postCreateForm(u, form).Body.String())
	if p["response_variants_json"] != form.Get("response_variants_json") {
		t.Errorf("prefill response_variants_json = %#v", p["response_variants_json"])
	}
	if _, has := p["response_rules_json"]; has {
		t.Errorf("empty textarea should be omitted, got %#v", p["response_rules_json"])
	}
	if _, has := p["response_variants"]; has {
		t.Error("parsed variants must not be sent alongside the raw text")
	}
}

// Service-side validation (bad slug format) is the most common error once
// custom slugs exist.
func TestCreateFormValidationErrorKeepsInput(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{}, 50)
	w := postCreateForm(u, fullForm("x_bad"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 re-render", w.Code)
	}
	assertPrefillCarries(t, renderedPrefill(t, w.Body.String()), "x_bad")
}

func TestCreateFormRateLimitKeepsInput(t *testing.T) {
	addr := os.Getenv("QUICKMOCK_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("QUICKMOCK_TEST_REDIS_ADDR not set, skipping test that needs Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { rdb.Close() })
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis at %s: %v", addr, err)
	}
	store := &fakeCreateStore{}
	u := creatingUI(t, store, 50)
	u.writes = repository.NewRateLimiter(rdb, 0, time.Minute) // every write denied

	w := postCreateForm(u, fullForm("fresh-slug"))
	if w.Code != http.StatusOK || len(store.created) != 0 {
		t.Fatalf("status = %d, created = %d; want 200 re-render and nothing stored", w.Code, len(store.created))
	}
	assertPrefillCarries(t, renderedPrefill(t, w.Body.String()), "fresh-slug")
}

// An empty body is a deliberate choice and must not come back as the
// default example body.
func TestCreateFormKeepsEmptyBody(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{perIPUsed: 50}, 50)
	form := fullForm("fresh-slug")
	form.Set("response_body", "")
	p := renderedPrefill(t, postCreateForm(u, form).Body.String())
	if b, ok := p["response_body"]; !ok || b != "" {
		t.Fatalf("prefill response_body = %#v (present=%v), want empty string", b, ok)
	}
}

// The prefill sits inside <script>: user input must not be able to close it.
func TestCreateFormPrefillEscapesScript(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{perIPUsed: 50}, 50)
	form := fullForm("fresh-slug")
	payload := `</script><script>alert(1)</script>`
	form.Set("response_body", payload)
	form.Set("name", payload)

	page := postCreateForm(u, form).Body.String()
	if strings.Contains(page, "<script>alert(1)") {
		t.Fatal("user input broke out of the prefill <script>")
	}
	if p := renderedPrefill(t, page); p["response_body"] != payload {
		t.Fatalf("prefill response_body = %#v, want the payload verbatim", p["response_body"])
	}
}

// A plain visit must not get a prefill: only error re-renders and the
// guide/template CTAs set one.
func TestHomeHasNoPrefillByDefault(t *testing.T) {
	u := creatingUI(t, &fakeCreateStore{}, 50)
	w := httptest.NewRecorder()
	u.Home(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(w.Body.String(), "window.__qmPrefill = ") {
		t.Fatal("plain home page carries a prefill")
	}
}
