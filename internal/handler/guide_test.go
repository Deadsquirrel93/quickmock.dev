package handler

import (
	"encoding/json"
	"strings"
	"testing"

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
	if len(UseCases) != 8 {
		t.Fatalf("want 8 cases, got %d", len(UseCases))
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
