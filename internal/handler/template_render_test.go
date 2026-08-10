package handler

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

// payloadBlockRe extracts the contents of the payload <pre><code> block so
// tests can assert on the response-body preview specifically, without
// tripping over unrelated markup elsewhere on the page. html/template
// escapes the quotes in that content (e.g. " -> &#34;), so the match is
// unescaped back to plain text before the caller inspects it.
var payloadBlockRe = regexp.MustCompile(`(?s)<code x-ref="payloadCode">(.*?)</code>`)

func payloadBlock(t *testing.T, body string) string {
	t.Helper()
	m := payloadBlockRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("page missing the payload code block")
	}
	return html.UnescapeString(m[1])
}

func TestTemplateIndexRenders(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.Templates(w, httptest.NewRequest("GET", "/templates", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := html.UnescapeString(w.Body.String())
	for _, tpl := range MockTemplates {
		if !strings.Contains(body, "/templates/"+tpl.Slug) {
			t.Fatalf("index missing link to %s", tpl.Slug)
		}
	}
	for _, c := range TemplateCategories {
		title := u.localz.T("en", "templates.category."+string(c))
		if !strings.Contains(body, title) {
			t.Fatalf("index missing category heading %q", title)
		}
	}
}

func TestTemplateIndexJSONLD(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	js := string(TemplateIndexJSONLD(localz, "en", "https://example.test"))
	if !strings.Contains(js, "ItemList") {
		t.Fatalf("JSON-LD missing ItemList in %s", js)
	}

	type graphNode struct {
		Type            string `json:"@type"`
		ItemListElement []any  `json:"itemListElement"`
	}
	var payload struct {
		Graph []graphNode `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(js), &payload); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v", err)
	}

	var found bool
	for _, node := range payload.Graph {
		if node.Type != "ItemList" {
			continue
		}
		found = true
		if got := len(node.ItemListElement); got != len(MockTemplates) {
			t.Fatalf("ItemList has %d ListItem entries, want %d", got, len(MockTemplates))
		}
	}
	if !found {
		t.Fatal("JSON-LD @graph missing the ItemList node")
	}
}

func TestTemplatesByCategory(t *testing.T) {
	total := 0
	seen := make(map[string]TemplateCategory)
	for _, c := range TemplateCategories {
		for _, tpl := range TemplatesByCategory(c) {
			if prev, ok := seen[tpl.Slug]; ok {
				t.Fatalf("template %q appears in both %q and %q", tpl.Slug, prev, c)
			}
			seen[tpl.Slug] = c
		}
		total += len(TemplatesByCategory(c))
	}
	if total != len(MockTemplates) {
		t.Fatalf("sum of category lengths = %d, want %d", total, len(MockTemplates))
	}
}

func TestTemplateCaseRenders(t *testing.T) {
	u := testUI(t)
	for _, tpl := range MockTemplates {
		t.Run(tpl.Slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := withSlug(httptest.NewRequest("GET", "/templates/"+tpl.Slug, nil), tpl.Slug)
			u.TemplateCase(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			body := w.Body.String()
			title := u.localz.T("en", tpl.KeyPrefix+".title")
			if !strings.Contains(body, title) {
				t.Fatalf("page missing title %q", title)
			}
			if !strings.Contains(body, `action="/templates/`+tpl.Slug+`/create"`) {
				t.Fatal("page missing the create form action")
			}
			if !strings.Contains(body, "?prefill_template="+tpl.Slug) {
				t.Fatal("page missing the prefill_template link")
			}
			for _, path := range tpl.Fields {
				if !strings.Contains(body, path) {
					t.Fatalf("page missing field path %q", path)
				}
			}
			// The payload block must show the pretty-printed response body
			// (what the Fields table's JSON paths address), not the
			// escaped-quote string that POST /api/mocks' response_body
			// carries.
			block := payloadBlock(t, body)
			if strings.Contains(block, `\"`) {
				t.Fatalf("payload block still shows an escaped-quote string: %s", block)
			}
			if tpl.Slug == "stripe-webhook" {
				if !strings.Contains(block, `"amount": 4999`) {
					t.Fatalf("payload block missing the response body's amount field: %s", block)
				}
				if !strings.Contains(block, `"object": "payment_intent"`) {
					t.Fatalf("payload block missing the response body's object field: %s", block)
				}
			}
		})
	}
}

func TestTemplateCaseUnknownSlug(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	req := withSlug(httptest.NewRequest("GET", "/templates/nope", nil), "nope")
	u.TemplateCase(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestTemplateCreateUnknownSlug covers only the 404 branch: an unknown slug
// never reaches u.svc.Create, so this is safe to run against testUI's
// service-less fixture. The success path needs a real MockService (a live
// Postgres pool behind it) and is verified manually, not in this package.
func TestTemplateCreateUnknownSlug(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	req := withSlug(httptest.NewRequest("POST", "/templates/nope/create", nil), "nope")
	u.TemplateCreate(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestTemplatePrefill(t *testing.T) {
	js, ok := templatePrefill("stripe-webhook")
	if !ok {
		t.Fatal("known slug must produce prefill")
	}
	if !strings.Contains(string(js), "response_body") {
		t.Fatalf("prefill missing the template config: %s", js)
	}
	if _, ok := templatePrefill("does-not-exist"); ok {
		t.Fatal("unknown slug must not produce prefill")
	}
	if _, ok := templatePrefill(""); ok {
		t.Fatal("empty slug must not produce prefill")
	}
}

func TestHomePrefillTemplateQuery(t *testing.T) {
	u := testUI(t)

	w := httptest.NewRecorder()
	u.Home(w, httptest.NewRequest("GET", "/?prefill_template=stripe-webhook", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "payment_intent.succeeded") {
		t.Fatal("page missing the template's prefill payload")
	}

	w = httptest.NewRecorder()
	u.Home(w, httptest.NewRequest("GET", "/?prefill_template=does-not-exist", nil))
	if strings.Contains(w.Body.String(), "window.__qmPrefill =") {
		t.Fatal("unknown template slug must not set window.__qmPrefill")
	}
}

func TestHomePrefillGuideWinsOverTemplate(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.Home(w, httptest.NewRequest("GET", "/?prefill=simulate-slow-api&prefill_template=stripe-webhook", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "response_delay_ms") {
		t.Fatal("guide prefill must win when both params are present")
	}
	if strings.Contains(body, "payment_intent.succeeded") {
		t.Fatal("template prefill must not appear when the guide prefill wins")
	}
}

func TestTemplateCaseJSONLD(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	tpl, ok := TemplateBySlug("stripe-webhook")
	if !ok {
		t.Fatal("stripe-webhook template must be registered")
	}
	js := string(TemplateCaseJSONLD(localz, "en", "https://example.test", tpl))
	for _, want := range []string{"HowTo", "BreadcrumbList", "/templates/stripe-webhook"} {
		if !strings.Contains(js, want) {
			t.Fatalf("JSON-LD missing %q in %s", want, js)
		}
	}
	var v any
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v", err)
	}
}

func TestPrettyPayload(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bare token as a value",
			in:   `{"created":{{now.unix}}}`,
			want: "{\n  \"created\": {{now.unix}}\n}",
		},
		{
			name: "token inside a string",
			in:   `{"email":"{{faker.email}}"}`,
			want: "{\n  \"email\": \"{{faker.email}}\"\n}",
		},
		{
			name: "unformattable input is returned unchanged",
			in:   `{"a":1,}`,
			want: `{"a":1,}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prettyPayload(tt.in)
			if got != tt.want {
				t.Fatalf("prettyPayload(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTemplateCaseNoRawKeys(t *testing.T) {
	u := testUI(t)
	for _, tpl := range MockTemplates {
		t.Run(tpl.Slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := withSlug(httptest.NewRequest("GET", "/templates/"+tpl.Slug, nil), tpl.Slug)
			u.TemplateCase(w, req)
			if strings.Contains(w.Body.String(), "templates.case.") {
				t.Fatal("page leaked a raw locale key")
			}
		})
	}
}
