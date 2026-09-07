package handler

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

func testUI(t *testing.T) *UI {
	t.Helper()
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	webSub, err := fs.Sub(quickmock.WebFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer(webSub, localz, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	return &UI{renderer: r, localz: localz, baseURL: "https://example.test"}
}

func withSlug(r *http.Request, slug string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", slug)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestGuideIndexRenders(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.Guide(w, httptest.NewRequest("GET", "/guide", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, c := range UseCases {
		if !strings.Contains(body, "/guide/"+c.Slug) {
			t.Fatalf("index missing link to %s", c.Slug)
		}
	}
}

func TestGuideCaseRenders(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	req := withSlug(httptest.NewRequest("GET", "/guide/mock-webhook-receiver", nil), "mock-webhook-receiver")
	u.GuideCase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "/api/mocks") {
		t.Fatal("case page missing the create curl")
	}
	if !strings.Contains(body, "application/ld+json") {
		t.Fatal("case page missing JSON-LD")
	}
	if !strings.Contains(body, "navigator.clipboard") {
		t.Fatal("case page missing copy buttons")
	}
	if !strings.Contains(body, "/?prefill=mock-webhook-receiver#create") {
		t.Fatal("case page CTA missing the prefill link")
	}
	if !strings.Contains(body, "/guide/echo-request-data") {
		t.Fatal("case page missing sibling guide link")
	}
	if !strings.Contains(body, "/templates/stripe-webhook") {
		t.Fatal("case page missing related template link")
	}
}

func TestUseCaseRelatedIsWellFormed(t *testing.T) {
	inbound := make(map[string]bool, len(UseCases))
	for _, c := range UseCases {
		t.Run(c.Slug, func(t *testing.T) {
			if len(c.Related) < 3 {
				t.Fatalf("Related has %d entries, want >= 3", len(c.Related))
			}
			seen := make(map[string]bool, len(c.Related))
			for _, slug := range c.Related {
				if slug == c.Slug {
					t.Fatalf("Related contains self-link %q", slug)
				}
				if seen[slug] {
					t.Fatalf("Related contains duplicate slug %q", slug)
				}
				seen[slug] = true
				if _, ok := UseCaseBySlug(slug); !ok {
					t.Fatalf("Related slug %q does not resolve via UseCaseBySlug", slug)
				}
			}
		})
		for _, slug := range c.Related {
			inbound[slug] = true
		}
	}
	for _, c := range UseCases {
		if !inbound[c.Slug] {
			t.Fatalf("case %q has no inbound Related link from any other case", c.Slug)
		}
	}
}

func TestRelatedUseCasesResolves(t *testing.T) {
	got := RelatedUseCases("mock-rest-api")
	if len(got) != 3 {
		t.Fatalf("RelatedUseCases(mock-rest-api) len = %d, want 3", len(got))
	}
	if got := RelatedUseCases("no-such-slug"); got != nil {
		t.Fatalf("RelatedUseCases(no-such-slug) = %v, want nil", got)
	}
}

func TestTemplatesForGuide(t *testing.T) {
	got := TemplatesForGuide("mock-webhook-receiver")
	if len(got) == 0 {
		t.Fatal("TemplatesForGuide(mock-webhook-receiver) is empty, want at least one template")
	}
	for _, tpl := range got {
		if tpl.RelatedGuide != "mock-webhook-receiver" {
			t.Fatalf("template %q has RelatedGuide = %q, want mock-webhook-receiver", tpl.Slug, tpl.RelatedGuide)
		}
	}
	if got := TemplatesForGuide("simulate-slow-api"); len(got) != 0 {
		t.Fatalf("TemplatesForGuide(simulate-slow-api) len = %d, want 0", len(got))
	}
}

func TestGuideCaseNotFound(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	req := withSlug(httptest.NewRequest("GET", "/guide/nope", nil), "nope")
	u.GuideCase(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHomeLinksIntoGuidesAndTemplates(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.Home(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"/guide/mock-rest-api",
		"/guide/mock-webhook-receiver",
		"/templates/stripe-webhook",
		"/templates/oauth2-token-response",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("home page missing link to %s", want)
		}
	}
	if !strings.Contains(body, `href="/templates"`) {
		t.Fatal("home page missing the templates hub link")
	}
}

func TestFeaturedSlugsResolve(t *testing.T) {
	guides := FeaturedGuides()
	if len(guides) != len(FeaturedGuideSlugs) {
		t.Fatalf("FeaturedGuides() len = %d, want %d", len(guides), len(FeaturedGuideSlugs))
	}
	for _, slug := range FeaturedGuideSlugs {
		if _, ok := UseCaseBySlug(slug); !ok {
			t.Fatalf("FeaturedGuideSlugs entry %q does not resolve via UseCaseBySlug", slug)
		}
	}

	templates := FeaturedTemplates()
	if len(templates) != len(FeaturedTemplateSlugs) {
		t.Fatalf("FeaturedTemplates() len = %d, want %d", len(templates), len(FeaturedTemplateSlugs))
	}
	for _, slug := range FeaturedTemplateSlugs {
		if _, ok := TemplateBySlug(slug); !ok {
			t.Fatalf("FeaturedTemplateSlugs entry %q does not resolve via TemplateBySlug", slug)
		}
	}
}
