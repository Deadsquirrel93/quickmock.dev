package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPISpecIsValidAndCurrent(t *testing.T) {
	w := httptest.NewRecorder()
	OpenAPISpec("https://example.test/")(w, httptest.NewRequest("GET", "/openapi.json", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected response: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	info, _ := spec["info"].(map[string]any)
	paths, _ := spec["paths"].(map[string]any)
	if spec["openapi"] != "3.1.0" || info["version"] != LastUpdated {
		t.Fatalf("unexpected OpenAPI metadata: %+v", spec)
	}
	for _, path := range []string{"/api/mocks", "/api/mocks/{slug}", "/api/parse-openapi"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI contract missing %s", path)
		}
	}
}

func TestOpenAPIDocumentsCustomSlug(t *testing.T) {
	w := httptest.NewRecorder()
	OpenAPISpec("https://example.test/")(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	props := spec["components"].(map[string]any)["schemas"].(map[string]any)["MockInput"].(map[string]any)["properties"].(map[string]any)
	slug, ok := props["slug"].(map[string]any)
	if !ok {
		t.Fatal("MockInput schema has no slug property")
	}
	if slug["type"] != "string" || slug["minLength"] != float64(4) || slug["maxLength"] != float64(48) {
		t.Errorf("slug schema = %+v", slug)
	}
	// Mock (the response) is allOf MockInput, so a pattern here would also
	// constrain responses, and generated mixed-case slugs (GdJEdL9ZgxkR)
	// would fail strict validators. The rule lives in the description.
	if _, has := slug["pattern"]; has {
		t.Errorf("slug must not carry a pattern: it leaks into the Mock response schema")
	}
	if d, _ := slug["description"].(string); !strings.Contains(d, "lowercase") {
		t.Errorf("slug description %q must state the format rule", d)
	}
	if d, _ := slug["description"].(string); !strings.Contains(d, "create-only") {
		t.Errorf("slug description %q must say it is create-only", d)
	}
	post := spec["paths"].(map[string]any)["/api/mocks"].(map[string]any)["post"].(map[string]any)
	if _, ok := post["responses"].(map[string]any)["409"]; !ok {
		t.Error("POST /api/mocks does not document the 409 slug_taken response")
	}
}

func TestDocsPageRendersCoreExamples(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.Docs(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	for _, want := range []string{"/openapi.json", "X-Quickmock-Variant", "response_rules", "logs_public"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("docs page missing %q", want)
		}
	}
}

func TestSitemapIncludesDocsAndReciprocalLocales(t *testing.T) {
	w := httptest.NewRecorder()
	SitemapXML("https://example.test", []string{"en", "ru"}, "en")(w, httptest.NewRequest("GET", "/sitemap.xml", nil))
	body := w.Body.String()
	for _, want := range []string{
		"<loc>https://example.test/docs</loc>",
		"<loc>https://example.test/docs?lang=ru</loc>",
		"hreflang=\"en\" href=\"https://example.test/docs\"",
		"hreflang=\"ru\" href=\"https://example.test/docs?lang=ru\"",
		"<lastmod>" + LastUpdated + "</lastmod>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sitemap missing %q", want)
		}
	}
}
