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

// TestHomeJSONLDUnchanged pins HomeJSONLD's byte-exact output. faqNode is
// carved out of HomeJSONLD in this same change, and this output must not
// move a single byte: the FAQ pairs on the home page stay its own
// seo.faq.q1..q5/a1..a5 data, only the node-building code moves.
func TestHomeJSONLDUnchanged(t *testing.T) {
	localz := testLocalizer(t)
	got := string(HomeJSONLD(localz, "en", "https://example.test", []string{"en", "ru"}))
	want := `{"@context":"https://schema.org","@graph":[{"@id":"https://example.test/#app","@type":"WebApplication","applicationCategory":"DeveloperApplication","author":{"@type":"Person","name":"Nikita Chernykh","url":"https://www.linkedin.com/in/chernykh-nikita/"},"browserRequirements":"Requires JavaScript-capable browser","dateModified":"2026-09-08","description":"Quickmock — free HTTP mock API in 30 seconds. Pick method, status, body — get a public URL. Live request inspector. No signup, no tracking, open source.","featureList":"Free HTTP mock endpoints; custom status codes and headers; configurable response delay; live request inspector; no signup; open source; no tracking","inLanguage":["en","ru"],"isAccessibleForFree":true,"name":"Quickmock","offers":{"@type":"Offer","price":"0","priceCurrency":"USD"},"operatingSystem":"Any (web)","publisher":{"@id":"https://example.test/#org"},"url":"https://example.test/"},{"@id":"https://example.test/#org","@type":"Organization","logo":"https://example.test/static/favicon.svg","name":"Quickmock","sameAs":["https://github.com/Deadsquirrel93/quickmock.dev","https://www.linkedin.com/in/chernykh-nikita/","https://t.me/deadsquirrel93","https://boosty.to/deadsquirrel93"],"url":"https://example.test/"},{"@type":"HowTo","inLanguage":"en","name":"How to create a mock API in 30 seconds","step":[{"@type":"HowToStep","name":"Fill out the form below — method, response body, status code.","position":1,"text":"Fill out the form below — method, response body, status code."},{"@type":"HowToStep","name":"Click \"Create Mock\" and you'll get a public URL like /m/abc123def456.","position":2,"text":"Click \"Create Mock\" and you'll get a public URL like /m/abc123def456."},{"@type":"HowToStep","name":"Use the URL in your code. Open the mock page to watch requests live.","position":3,"text":"Use the URL in your code. Open the mock page to watch requests live."}],"totalTime":"PT30S"},{"@type":"FAQPage","mainEntity":[{"@type":"Question","acceptedAnswer":{"@type":"Answer","text":"Quickmock is a free online tool that creates a public HTTP mock endpoint in 30 seconds. You paste a response body, pick a method and status code, and get a URL you can call from any client — curl, Postman, your frontend, a CI test, anywhere."},"name":"What is Quickmock?"},{"@type":"Question","acceptedAnswer":{"@type":"Answer","text":"No. There is no account, no email, no credit card. Mocks are limited per IP and expire after the TTL you pick (up to 30 days). The service is free and open source."},"name":"Do I need to sign up or pay?"},{"@type":"Question","acceptedAnswer":{"@type":"Answer","text":"Frontend prototyping while the backend is not ready, webhook testing, integration and contract tests, reproducing edge cases like 500 errors or slow responses, API demos, and teaching examples."},"name":"What can I use Quickmock for?"},{"@type":"Question","acceptedAnswer":{"@type":"Answer","text":"Yes. Every mock has a private live inspector for incoming method, path, headers, query, and body. Unlock it with the mock's admin token; body and sender-IP capture can be disabled."},"name":"Can I see who calls my mock?"},{"@type":"Question","acceptedAnswer":{"@type":"Answer","text":"Yes. Drop tokens like {{faker.uuid}}, {{faker.name}}, {{faker.email}}, or {{now.iso8601}} into the response body — the mock stores the template as-is and substitutes fresh values on every hit. request.* tokens echo the incoming request back: {{request.query.id}}, {{request.header.x-request-id}}, {{request.body.user.name}}. The full list (people, IDs, network, text, time, request echo) is in the form under \"Dynamic tokens\"."},"name":"Can mocks return dynamic data?"}]}]}`
	if got != want {
		t.Fatalf("HomeJSONLD output changed:\ngot:  %s\nwant: %s", got, want)
	}
}
