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

func TestGuideIndexJSONLD(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	js := string(GuideIndexJSONLD(localz, "en", "https://example.test"))

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

	var foundList, foundBreadcrumb bool
	for _, node := range payload.Graph {
		switch node.Type {
		case "ItemList":
			foundList = true
			if got := len(node.ItemListElement); got != len(UseCases) {
				t.Fatalf("ItemList has %d ListItem entries, want %d", got, len(UseCases))
			}
		case "BreadcrumbList":
			foundBreadcrumb = true
		}
	}
	if !foundList {
		t.Fatal("JSON-LD @graph missing the ItemList node")
	}
	if !foundBreadcrumb {
		t.Fatal("JSON-LD @graph missing the BreadcrumbList node")
	}
}

func TestDocsJSONLD(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	js := string(DocsJSONLD(localz, "en", "https://example.test"))

	type graphNode struct {
		Type string `json:"@type"`
	}
	var payload struct {
		Graph []graphNode `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(js), &payload); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v", err)
	}

	var found bool
	for _, node := range payload.Graph {
		if node.Type == "TechArticle" {
			found = true
		}
	}
	if !found {
		t.Fatal("JSON-LD @graph missing the TechArticle node")
	}
}

func TestChangelogJSONLD(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}
	js := string(ChangelogJSONLD(localz, "en", "https://example.test"))

	type graphNode struct {
		Type            string `json:"@type"`
		SoftwareVersion string `json:"softwareVersion"`
	}
	var payload struct {
		Graph []graphNode `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(js), &payload); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v", err)
	}

	var found bool
	for _, node := range payload.Graph {
		if node.Type != "SoftwareApplication" {
			continue
		}
		found = true
		if node.SoftwareVersion != LastUpdated {
			t.Fatalf("softwareVersion = %q, want %q", node.SoftwareVersion, LastUpdated)
		}
	}
	if !found {
		t.Fatal("JSON-LD @graph missing the SoftwareApplication node")
	}
}

func TestHubsEmitJSONLD(t *testing.T) {
	u := testUI(t)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"guide", u.Guide, "/guide"},
		{"docs", u.Docs, "/docs"},
		{"changelog", u.Changelog, "/changelog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handler(w, httptest.NewRequest("GET", tt.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), "application/ld+json") {
				t.Fatalf("%s page missing JSON-LD", tt.path)
			}
		})
	}
}
