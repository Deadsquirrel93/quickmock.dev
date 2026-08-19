package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func testSpamFilter(t *testing.T, patterns, allow []string) *SpamFilter {
	t.Helper()
	f, err := NewSpamFilter(patterns, allow, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func defaultFilter(t *testing.T) *SpamFilter {
	t.Helper()
	pats, err := LoadSpamPatterns("")
	if err != nil {
		t.Fatal(err)
	}
	return testSpamFilter(t, pats, nil)
}

func TestDefaultPatternsBlockSpam(t *testing.T) {
	f := defaultFilter(t)
	spam := []string{
		"Buy cheap viagra online today",
		"Best online casino — 100 free spins no deposit bonus",
		"we sell quality backlinks for your site",
		"Double your bitcoin in 24 hours, guaranteed profit",
		"make $5,000 per week from home",
		"Location: gopher://example.test/_payload",
		"fetch http://169.254.169.254/latest/meta-data/",
		`<add modules="FastCgiModule" scriptProcessor="php-cgi.exe" />`,
		`Website: <a href="https://example.test/">Example</a>`,
	}
	for _, s := range spam {
		in := model.MockInput{ResponseBody: s}
		if !f.Blocked(&in, "203.0.113.9") {
			t.Errorf("expected spam to be blocked: %q", s)
		}
	}
}

func TestDefaultPatternsAllowLegitPayloads(t *testing.T) {
	f := defaultFilter(t)
	legit := []string{
		`{"movie":"Casino Royale","rating":8}`,
		`{"user":"alice","email":"alice@example.com"}`,
		`{"order":{"id":42,"items":[{"sku":"A-1","qty":2}]}}`,
		"spin the wheel to win",
		"the profit margin was 12% in Q2",
	}
	for _, s := range legit {
		in := model.MockInput{ResponseBody: s}
		if f.Blocked(&in, "203.0.113.9") {
			t.Errorf("false positive on: %q", s)
		}
	}
}

func TestBlockedScansAllUserFields(t *testing.T) {
	f := testSpamFilter(t, []string{`(?i)spamword`}, nil)
	cases := map[string]model.MockInput{
		"name":         {Name: "spamword"},
		"body":         {ResponseBody: "xx spamword xx"},
		"header value": {ResponseHeaders: map[string]string{"X-Promo": "spamword"}},
		"path suffix":  {PathSuffix: "spamword/deal"},
		"error body":   {ErrorResponse: &model.ResponseStep{Body: "spamword"}},
		"seq body":     {SequenceSteps: []model.ResponseStep{{Body: "spamword"}}},
		"seq header":   {SequenceSteps: []model.ResponseStep{{Headers: map[string]string{"X": "spamword"}}}},
		"variant body": {Variants: []model.NamedVariant{{Body: "spamword"}}},
		"route body":   {Routes: []model.MockRoute{{ResponseBody: "spamword"}}},
		"route variant": {Routes: []model.MockRoute{{Variants: []model.NamedVariant{{
			Body: "spamword",
		}}}}},
	}
	for field, in := range cases {
		in := in
		if !f.Blocked(&in, "203.0.113.9") {
			t.Errorf("%s not scanned", field)
		}
	}
}

func TestAllowlistSkipsFilter(t *testing.T) {
	f := testSpamFilter(t, []string{`(?i)spamword`}, []string{"203.0.113.9", "10.0.0.0/8"})
	in := model.MockInput{ResponseBody: "spamword"}
	if f.Blocked(&in, "203.0.113.9") {
		t.Error("exact allowlisted IP must pass")
	}
	if f.Blocked(&in, "10.1.2.3") {
		t.Error("CIDR-allowlisted IP must pass")
	}
	if !f.Blocked(&in, "198.51.100.7") {
		t.Error("other IPs must still be blocked")
	}
}

func TestLoadSpamPatternsModes(t *testing.T) {
	if pats, err := LoadSpamPatterns("off"); err != nil || pats != nil {
		t.Fatalf("off => nil, nil; got %v, %v", pats, err)
	}
	pats, err := LoadSpamPatterns("")
	if err != nil || len(pats) == 0 {
		t.Fatalf("empty path must load embedded defaults; got %d, %v", len(pats), err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "patterns.txt")
	os.WriteFile(file, []byte("# comment\n\n(?i)custom-junk\n"), 0o600)
	pats, err = LoadSpamPatterns(file)
	if err != nil || len(pats) != 1 || pats[0] != "(?i)custom-junk" {
		t.Fatalf("file parse wrong: %v, %v", pats, err)
	}
	if _, err := LoadSpamPatterns(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("missing file must error (fail fast)")
	}
}

func TestNewSpamFilterRejectsInvalidInput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewSpamFilter([]string{"("}, nil, logger); err == nil {
		t.Fatal("invalid regex must error")
	}
	if _, err := NewSpamFilter(nil, []string{"not-an-ip"}, logger); err == nil {
		t.Fatal("invalid allow IP must error")
	}
}

func TestNilAndEmptyFilterBlockNothing(t *testing.T) {
	var nilFilter *SpamFilter
	in := model.MockInput{ResponseBody: "buy cheap viagra online"}
	if nilFilter.Blocked(&in, "1.2.3.4") {
		t.Error("nil filter must be a no-op")
	}
	empty := testSpamFilter(t, nil, nil)
	if empty.Blocked(&in, "1.2.3.4") {
		t.Error("empty filter (off) must be a no-op")
	}
}

func TestCreateRejectsSpamBeforeStorage(t *testing.T) {
	f := testSpamFilter(t, []string{`(?i)spamword`}, nil)
	s := NewMockService(nil, nil, nil, 1024, 10, time.Hour, 720*time.Hour, f)
	_, err := s.Create(context.Background(), model.MockInput{
		Method:       model.MethodGET,
		ResponseBody: "spamword",
	}, "198.51.100.7")
	if !errors.Is(err, ErrSpamBlocked) {
		t.Fatalf("err = %v, want ErrSpamBlocked", err)
	}
}
