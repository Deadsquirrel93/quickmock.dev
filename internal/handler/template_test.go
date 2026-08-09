package handler

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

func TestMockTemplatesIntegrity(t *testing.T) {
	if len(MockTemplates) != 10 {
		t.Fatalf("want 10 templates, got %d", len(MockTemplates))
	}
	validCategory := map[TemplateCategory]bool{
		CategoryPayments: true, CategoryDevtools: true, CategoryAuth: true, CategoryGeneric: true,
	}
	validKind := map[TemplateKind]bool{
		KindPayload: true, KindResponder: true, KindAPI: true,
	}
	seen := map[string]bool{}
	for _, tpl := range MockTemplates {
		if tpl.Slug == "" {
			t.Fatal("template slug must not be empty")
		}
		if seen[tpl.Slug] {
			t.Fatalf("duplicate slug %q", tpl.Slug)
		}
		seen[tpl.Slug] = true
		if tpl.KeyPrefix != "templates.case."+tpl.Slug {
			t.Fatalf("%s: KeyPrefix = %q, want templates.case.%s", tpl.Slug, tpl.KeyPrefix, tpl.Slug)
		}
		if !validCategory[tpl.Category] {
			t.Fatalf("%s: unknown category %q", tpl.Slug, tpl.Category)
		}
		if !validKind[tpl.Kind] {
			t.Fatalf("%s: unknown kind %q", tpl.Slug, tpl.Kind)
		}
		if n := len(tpl.Fields); n < 1 || n > 5 {
			t.Fatalf("%s: len(Fields) = %d, want 1..5", tpl.Slug, n)
		}
		if tpl.Expect == "" {
			t.Fatalf("%s: Expect must not be empty", tpl.Slug)
		}
		if tpl.CallVerb == "" {
			t.Fatalf("%s: CallVerb must not be empty", tpl.Slug)
		}
		if tpl.RelatedGuide != "" {
			if _, ok := UseCaseBySlug(tpl.RelatedGuide); !ok {
				t.Fatalf("%s: RelatedGuide %q does not resolve to a guide case", tpl.Slug, tpl.RelatedGuide)
			}
		}
	}
}

func TestTemplateSlugsDoNotCollideWithGuides(t *testing.T) {
	guideSlugs := map[string]bool{}
	for _, c := range UseCases {
		guideSlugs[c.Slug] = true
	}
	for _, tpl := range MockTemplates {
		if guideSlugs[tpl.Slug] {
			t.Fatalf("template slug %q collides with a /guide slug", tpl.Slug)
		}
	}
}

func TestTemplateCreateBodyParses(t *testing.T) {
	pathSuffixSlugs := map[string]bool{
		"openid-configuration": true,
		"jwks-endpoint":        true,
	}
	for _, tpl := range MockTemplates {
		in, ok := TemplateInput(tpl.Slug)
		if !ok {
			t.Fatalf("%s: TemplateInput must find the template", tpl.Slug)
		}
		if !model.ValidMethod(string(in.Method)) {
			t.Fatalf("%s: method %q invalid", tpl.Slug, in.Method)
		}
		if in.ResponseBody == "" {
			t.Fatalf("%s: ResponseBody must not be empty", tpl.Slug)
		}
		if pathSuffixSlugs[tpl.Slug] && in.PathSuffix == "" {
			t.Fatalf("%s: PathSuffix must not be empty", tpl.Slug)
		}
	}
}

func TestTemplatesPassSpamFilter(t *testing.T) {
	pats, err := service.LoadSpamPatterns("")
	if err != nil {
		t.Fatal(err)
	}
	f, err := service.NewSpamFilter(pats, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range MockTemplates {
		in, ok := TemplateInput(tpl.Slug)
		if !ok {
			t.Fatalf("%s: TemplateInput must find the template", tpl.Slug)
		}
		if f.Blocked(&in, "203.0.113.1") {
			t.Fatalf("%s: must not be blocked by the default spam filter", tpl.Slug)
		}
	}
}

func TestTemplateBySlug(t *testing.T) {
	if _, ok := TemplateBySlug("stripe-webhook"); !ok {
		t.Fatal("known slug must resolve")
	}
	if _, ok := TemplateBySlug("does-not-exist"); ok {
		t.Fatal("unknown slug must miss")
	}
}
