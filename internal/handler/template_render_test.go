package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

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
