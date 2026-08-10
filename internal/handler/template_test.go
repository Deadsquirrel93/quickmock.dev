package handler

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// templateCommonLocaleKeys are the non-per-template keys the /templates
// gallery pages depend on, shared across every template and the index page.
var templateCommonLocaleKeys = []string{
	"nav.templates",
	"templates.title",
	"templates.intro",
	"templates.meta_description",
	"templates.breadcrumb.templates",
	"templates.category.payments",
	"templates.category.devtools",
	"templates.category.auth",
	"templates.category.generic",
	"templates.kind.payload",
	"templates.kind.responder",
	"templates.kind.api",
	"templates.section.payload",
	"templates.section.fields",
	"templates.section.create",
	"templates.section.call",
	"templates.section.expect",
	"templates.section.differences",
	"templates.fields.col_field",
	"templates.fields.col_meaning",
	"templates.cta.title",
	"templates.cta.button",
	"templates.cta.open_in_form",
	"templates.back",
	"templates.related_guide",
}

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

// TestJWKSModulusDecodes guards against a structurally invalid "n": strict
// JOSE libraries fail to even parse a JWK whose base64url modulus doesn't
// decode, before they ever get to checking a signature — which defeats the
// whole point of this template (exercising a client's JWKS parsing). n must
// decode as unpadded base64url to exactly 256 bytes (an RSA-2048 modulus).
func TestJWKSModulusDecodes(t *testing.T) {
	in, ok := TemplateInput("jwks-endpoint")
	if !ok {
		t.Fatal("jwks-endpoint template must resolve")
	}
	var doc struct {
		Keys []struct {
			N string `json:"n"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(in.ResponseBody), &doc); err != nil {
		t.Fatalf("ResponseBody is not valid JSON: %v", err)
	}
	if len(doc.Keys) != 1 {
		t.Fatalf("want exactly 1 key, got %d", len(doc.Keys))
	}
	decoded, err := base64.RawURLEncoding.DecodeString(doc.Keys[0].N)
	if err != nil {
		t.Fatalf("n does not decode as base64url: %v", err)
	}
	if len(decoded) != 256 {
		t.Fatalf("decoded modulus is %d bytes, want 256 (RSA-2048)", len(decoded))
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

func TestTemplateLocaleCoverage(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}

	// T falls back to the literal key when a translation is missing, so
	// "resolves" means both non-empty and different from the key itself.
	check := func(lang, key string) {
		t.Helper()
		got := localz.T(lang, key)
		if got == "" {
			t.Fatalf("%s: %q resolved to an empty string", lang, key)
		}
		if got == key {
			t.Fatalf("%s: %q did not resolve to a translation", lang, key)
		}
	}

	for _, lang := range localz.Supported() {
		for _, tpl := range MockTemplates {
			check(lang, tpl.KeyPrefix+".title")
			check(lang, tpl.KeyPrefix+".summary")
			check(lang, tpl.KeyPrefix+".differences")
			for _, field := range tpl.Fields {
				check(lang, tpl.KeyPrefix+".field."+field)
			}
		}
	}
}

func TestTemplateCommonLocaleKeys(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}

	for _, lang := range localz.Supported() {
		for _, key := range templateCommonLocaleKeys {
			got := localz.T(lang, key)
			if got == "" {
				t.Fatalf("%s: %q resolved to an empty string", lang, key)
			}
			if got == key {
				t.Fatalf("%s: %q did not resolve to a translation", lang, key)
			}
		}
	}
}
