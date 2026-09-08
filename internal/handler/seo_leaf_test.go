package handler

import (
	"encoding/json"
	"strings"
	"testing"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

// testLocalizer is a fresh en-loaded Localizer for the tests in this file.
func testLocalizer(t *testing.T) *i18n.Localizer {
	t.Helper()
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	return localz
}

// parseGraph decodes a JSON-LD document's top-level @graph array into
// generic nodes, first undoing the "</" defang applied by the JSON-LD
// builders (a no-op for json.Unmarshal since "\/" is already a legal JSON
// escape for "/", but the brief calls for reversing it explicitly).
func parseGraph(t *testing.T, js string) []map[string]any {
	t.Helper()
	unescaped := strings.ReplaceAll(js, `<\/`, "</")
	var payload struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(unescaped), &payload); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v", err)
	}
	return payload.Graph
}

func findNode(graph []map[string]any, typ string) map[string]any {
	for _, node := range graph {
		if node["@type"] == typ {
			return node
		}
	}
	return nil
}

func TestGuideCaseJSONLDFAQPage(t *testing.T) {
	localz := testLocalizer(t)
	c := UseCase{
		Slug:      "test-case-with-faq",
		KeyPrefix: "guide.case.test-case-with-faq",
		FAQ:       []string{"one", "two", "three"},
	}
	graph := parseGraph(t, string(GuideCaseJSONLD(localz, "en", "https://example.test", c)))

	faq := findNode(graph, "FAQPage")
	if faq == nil {
		t.Fatal("JSON-LD @graph missing the FAQPage node for a case with FAQ")
	}
	mainEntity, ok := faq["mainEntity"].([]any)
	if !ok {
		t.Fatalf("FAQPage.mainEntity is not an array: %#v", faq["mainEntity"])
	}
	if got, want := len(mainEntity), len(c.FAQ); got != want {
		t.Fatalf("FAQPage.mainEntity has %d entries, want %d", got, want)
	}
}

func TestGuideCaseJSONLDNoFAQ(t *testing.T) {
	localz := testLocalizer(t)
	c := UseCase{
		Slug:      "test-case-no-faq",
		KeyPrefix: "guide.case.test-case-no-faq",
	}
	graph := parseGraph(t, string(GuideCaseJSONLD(localz, "en", "https://example.test", c)))

	if findNode(graph, "FAQPage") != nil {
		t.Fatal("JSON-LD @graph must not contain a FAQPage node when FAQ is empty")
	}
}

func TestGuideCaseJSONLDDateModified(t *testing.T) {
	localz := testLocalizer(t)
	c := UseCase{
		Slug:      "test-case-datemod",
		KeyPrefix: "guide.case.test-case-datemod",
	}
	graph := parseGraph(t, string(GuideCaseJSONLD(localz, "en", "https://example.test", c)))

	howTo := findNode(graph, "HowTo")
	if howTo == nil {
		t.Fatal("JSON-LD @graph missing the HowTo node")
	}
	if got := howTo["dateModified"]; got != LastUpdated {
		t.Fatalf("HowTo.dateModified = %v, want %q", got, LastUpdated)
	}
}

func TestTemplateCaseJSONLDFAQPage(t *testing.T) {
	localz := testLocalizer(t)
	tpl := MockTemplate{
		Slug:      "test-template-with-faq",
		KeyPrefix: "templates.case.test-template-with-faq",
		FAQ:       []string{"one", "two"},
	}
	graph := parseGraph(t, string(TemplateCaseJSONLD(localz, "en", "https://example.test", tpl)))

	faq := findNode(graph, "FAQPage")
	if faq == nil {
		t.Fatal("JSON-LD @graph missing the FAQPage node for a template with FAQ")
	}
	mainEntity, ok := faq["mainEntity"].([]any)
	if !ok {
		t.Fatalf("FAQPage.mainEntity is not an array: %#v", faq["mainEntity"])
	}
	if got, want := len(mainEntity), len(tpl.FAQ); got != want {
		t.Fatalf("FAQPage.mainEntity has %d entries, want %d", got, want)
	}
}

func TestTemplateCaseJSONLDNoFAQ(t *testing.T) {
	localz := testLocalizer(t)
	tpl := MockTemplate{
		Slug:      "test-template-no-faq",
		KeyPrefix: "templates.case.test-template-no-faq",
	}
	graph := parseGraph(t, string(TemplateCaseJSONLD(localz, "en", "https://example.test", tpl)))

	if findNode(graph, "FAQPage") != nil {
		t.Fatal("JSON-LD @graph must not contain a FAQPage node when FAQ is empty")
	}
}

func TestTemplateCaseJSONLDDateModified(t *testing.T) {
	localz := testLocalizer(t)
	tpl := MockTemplate{
		Slug:      "test-template-datemod",
		KeyPrefix: "templates.case.test-template-datemod",
	}
	graph := parseGraph(t, string(TemplateCaseJSONLD(localz, "en", "https://example.test", tpl)))

	howTo := findNode(graph, "HowTo")
	if howTo == nil {
		t.Fatal("JSON-LD @graph missing the HowTo node")
	}
	if got := howTo["dateModified"]; got != LastUpdated {
		t.Fatalf("HowTo.dateModified = %v, want %q", got, LastUpdated)
	}
}

func TestTemplateCaseJSONLDCitation(t *testing.T) {
	localz := testLocalizer(t)
	tpl := MockTemplate{
		Slug:      "test-template-with-sources",
		KeyPrefix: "templates.case.test-template-with-sources",
		Sources: []Source{
			{URL: "https://example.test/rfc-a", Title: "RFC A"},
			{URL: "https://example.test/rfc-b", Title: "RFC B"},
		},
	}
	graph := parseGraph(t, string(TemplateCaseJSONLD(localz, "en", "https://example.test", tpl)))

	howTo := findNode(graph, "HowTo")
	if howTo == nil {
		t.Fatal("JSON-LD @graph missing the HowTo node")
	}
	citation, ok := howTo["citation"].([]any)
	if !ok {
		t.Fatalf("HowTo.citation is not an array: %#v", howTo["citation"])
	}
	if got, want := len(citation), len(tpl.Sources); got != want {
		t.Fatalf("HowTo.citation has %d entries, want %d", got, want)
	}
	for i, raw := range citation {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("citation[%d] is not an object: %#v", i, raw)
		}
		if entry["@type"] != "CreativeWork" {
			t.Fatalf("citation[%d].@type = %v, want CreativeWork", i, entry["@type"])
		}
		if entry["name"] != tpl.Sources[i].Title {
			t.Fatalf("citation[%d].name = %v, want %q", i, entry["name"], tpl.Sources[i].Title)
		}
		if entry["url"] != tpl.Sources[i].URL {
			t.Fatalf("citation[%d].url = %v, want %q", i, entry["url"], tpl.Sources[i].URL)
		}
	}
}

// TestHomeJSONLDGraph checks HomeJSONLD's structural shape rather than
// pinning its byte-exact output: the home page's dateModified tracks
// LastUpdated (bumped on every deploy) and its FAQ copy is free to be
// edited, so a byte-exact golden string would fail for reasons unrelated to
// faqNode being carved out of HomeJSONLD in this same change.
func TestHomeJSONLDGraph(t *testing.T) {
	localz := testLocalizer(t)
	graph := parseGraph(t, string(HomeJSONLD(localz, "en", "https://example.test", []string{"en", "ru"})))

	app := findNode(graph, "WebApplication")
	if app == nil {
		t.Fatal("JSON-LD @graph missing the WebApplication node")
	}
	if got := app["dateModified"]; got != LastUpdated {
		t.Fatalf("WebApplication.dateModified = %v, want %q", got, LastUpdated)
	}

	faq := findNode(graph, "FAQPage")
	if faq == nil {
		t.Fatal("JSON-LD @graph missing the FAQPage node")
	}
	mainEntity, ok := faq["mainEntity"].([]any)
	if !ok {
		t.Fatalf("FAQPage.mainEntity is not an array: %#v", faq["mainEntity"])
	}
	if got, want := len(mainEntity), 5; got != want {
		t.Fatalf("FAQPage.mainEntity has %d entries, want %d", got, want)
	}
	for i, raw := range mainEntity {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("mainEntity[%d] is not an object: %#v", i, raw)
		}
		name, _ := entry["name"].(string)
		if strings.TrimSpace(name) == "" {
			t.Errorf("mainEntity[%d].name is empty", i)
		}
		answer, ok := entry["acceptedAnswer"].(map[string]any)
		if !ok {
			t.Fatalf("mainEntity[%d].acceptedAnswer is not an object: %#v", i, entry["acceptedAnswer"])
		}
		text, _ := answer["text"].(string)
		if strings.TrimSpace(text) == "" {
			t.Errorf("mainEntity[%d].acceptedAnswer.text is empty", i)
		}
	}
}
