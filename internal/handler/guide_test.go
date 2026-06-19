package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestUseCaseBySlug(t *testing.T) {
	if _, ok := UseCaseBySlug("test-retry-logic"); !ok {
		t.Fatal("known slug must resolve")
	}
	if _, ok := UseCaseBySlug("does-not-exist"); ok {
		t.Fatal("unknown slug must miss")
	}
}

func TestUseCasesIntegrity(t *testing.T) {
	if len(UseCases) != 9 {
		t.Fatalf("want 9 cases, got %d", len(UseCases))
	}
	seen := map[string]bool{}
	for _, c := range UseCases {
		if seen[c.Slug] {
			t.Fatalf("duplicate slug %q", c.Slug)
		}
		seen[c.Slug] = true
		if c.KeyPrefix != "guide.case."+c.Slug {
			t.Fatalf("%s: KeyPrefix = %q, want guide.case.%s", c.Slug, c.KeyPrefix, c.Slug)
		}
		var req createMockRequest
		if err := json.Unmarshal([]byte(c.CreateBody), &req); err != nil {
			t.Fatalf("%s: CreateBody is not valid JSON: %v", c.Slug, err)
		}
		if !model.ValidMethod(strings.ToUpper(req.Method)) {
			t.Fatalf("%s: CreateBody method %q invalid", c.Slug, req.Method)
		}
		if c.CallVerb == "" {
			t.Fatalf("%s: CallVerb empty", c.Slug)
		}
	}
}

func TestGuideCaseJSONLD(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	c, _ := UseCaseBySlug("test-retry-logic")
	js := string(GuideCaseJSONLD(localz, "en", "https://example.test", c))
	for _, want := range []string{"HowTo", "BreadcrumbList", "/guide/test-retry-logic"} {
		if !strings.Contains(js, want) {
			t.Fatalf("JSON-LD missing %q in %s", want, js)
		}
	}
	var v any
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v", err)
	}
}

func TestGuidePrefill(t *testing.T) {
	js, ok := guidePrefill("simulate-slow-api")
	if !ok {
		t.Fatal("known slug must produce prefill")
	}
	if !strings.Contains(string(js), "response_delay_ms") {
		t.Fatalf("prefill missing the case config: %s", js)
	}
	if _, ok := guidePrefill("does-not-exist"); ok {
		t.Fatal("unknown slug must not produce prefill")
	}
	if _, ok := guidePrefill(""); ok {
		t.Fatal("empty slug must not produce prefill")
	}
}

func TestCORSGuideCase(t *testing.T) {
	c, ok := UseCaseBySlug("mock-api-with-cors")
	if !ok {
		t.Fatal("cors case must be registered")
	}
	if !strings.Contains(c.CreateBody, `"cors_enabled": true`) {
		t.Fatalf("cors case CreateBody must enable cors: %s", c.CreateBody)
	}
	js, ok := guidePrefill("mock-api-with-cors")
	if !ok || !strings.Contains(string(js), "cors_enabled") {
		t.Fatalf("cors prefill must carry cors_enabled: %s", js)
	}
}

func TestSitemapIncludesGuides(t *testing.T) {
	h := SitemapXML("https://example.test", []string{"en", "ru"}, "en")
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("GET", "/sitemap.xml", nil))
	body := w.Body.String()
	if !strings.Contains(body, "https://example.test/guide</loc>") {
		t.Fatal("sitemap missing /guide index")
	}
	for _, c := range UseCases {
		if !strings.Contains(body, "https://example.test/guide/"+c.Slug+"</loc>") {
			t.Fatalf("sitemap missing /guide/%s", c.Slug)
		}
	}
}
