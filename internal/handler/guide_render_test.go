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
	req := withSlug(httptest.NewRequest("GET", "/guide/test-retry-logic", nil), "test-retry-logic")
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
	if !strings.Contains(body, "/?prefill=test-retry-logic#create") {
		t.Fatal("case page CTA missing the prefill link")
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
