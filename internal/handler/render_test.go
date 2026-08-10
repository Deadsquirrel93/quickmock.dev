package handler

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

func TestAssetVersionStableAndContentSensitive(t *testing.T) {
	base := fstest.MapFS{
		"static/css/style.css": {Data: []byte("body{}")},
		"static/js/app.js":     {Data: []byte("console.log(1)")},
		// A non-static sibling must not influence the version.
		"templates/base.html": {Data: []byte("<html>")},
	}

	v1, err := assetVersion(base)
	if err != nil {
		t.Fatalf("assetVersion: %v", err)
	}
	if v1 == "" {
		t.Fatal("version is empty")
	}

	// Deterministic: the same tree hashes to the same version.
	again, err := assetVersion(base)
	if err != nil {
		t.Fatalf("assetVersion (again): %v", err)
	}
	if again != v1 {
		t.Fatalf("version not stable: %q != %q", v1, again)
	}

	// Changing a static file's content changes the version.
	changed := fstest.MapFS{
		"static/css/style.css": {Data: []byte("body{color:red}")},
		"static/js/app.js":     {Data: []byte("console.log(1)")},
		"templates/base.html":  {Data: []byte("<html>")},
	}
	v2, err := assetVersion(changed)
	if err != nil {
		t.Fatalf("assetVersion (changed): %v", err)
	}
	if v2 == v1 {
		t.Fatalf("version did not change when CSS changed: %q", v1)
	}

	// Changing only a non-static file does NOT change the version.
	templateOnly := fstest.MapFS{
		"static/css/style.css": {Data: []byte("body{}")},
		"static/js/app.js":     {Data: []byte("console.log(1)")},
		"templates/base.html":  {Data: []byte("<html lang=en>")},
	}
	v3, err := assetVersion(templateOnly)
	if err != nil {
		t.Fatalf("assetVersion (template-only): %v", err)
	}
	if v3 != v1 {
		t.Fatalf("version changed on non-static edit: %q != %q", v3, v1)
	}
}

func TestAssetVersionFromEmbeddedFS(t *testing.T) {
	webSub, err := fs.Sub(quickmock.WebFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	v, err := assetVersion(webSub)
	if err != nil {
		t.Fatalf("assetVersion: %v", err)
	}
	if len(v) != 12 {
		t.Fatalf("version length = %d, want 12 (%q)", len(v), v)
	}
}

// TestErrMsgPicksTheRightArg covers the bug this helper exists to prevent:
// errors.body_too_large and errors.mock_limit_reached each carry exactly one
// %d placeholder, for different limits, but the error banner has both
// numbers on hand. errMsg must forward only the one the key actually wants
// (never both — that leaves an fmt "%!(EXTRA ...)" tail, or silently
// substitutes the wrong limit since both are ints).
func TestErrMsgPicksTheRightArg(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		errKey      string
		maxBodyKB   int
		maxMocks    int
		wantContain string
	}{
		{
			name:        "body_too_large uses the body limit",
			errKey:      "body_too_large",
			maxBodyKB:   100,
			maxMocks:    50,
			wantContain: "100 KB",
		},
		{
			name:        "mock_limit_reached uses the mock limit, not the body limit",
			errKey:      "mock_limit_reached",
			maxBodyKB:   100,
			maxMocks:    50,
			wantContain: "50 active mocks",
		},
		{
			name:        "a key with no placeholder is returned as-is",
			errKey:      "spam_blocked",
			maxBodyKB:   100,
			maxMocks:    50,
			wantContain: localz.T("en", "errors.spam_blocked"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errMsg(localz, "en", tt.errKey, tt.maxBodyKB, tt.maxMocks)
			if strings.Contains(got, "%!(EXTRA") {
				t.Fatalf("errMsg(%q) leaked a fmt EXTRA tail: %q", tt.errKey, got)
			}
			if !strings.Contains(got, tt.wantContain) {
				t.Fatalf("errMsg(%q) = %q, want it to contain %q", tt.errKey, got, tt.wantContain)
			}
		})
	}
}

// TestErrMsgFormatsEveryErrorKey is the guard the hand-written table above
// cannot be: it walks every "errors.*" string in every locale, keeps the ones
// carrying a format verb, and checks errMsg fills it in. Add a placeholder to
// a locale message and forget the matching case in errMsg's switch and the
// banner renders a raw "%d" (T takes the no-args path) — or, if the switch
// hands over the wrong number of args, an fmt artefact like "%!d(MISSING)".
// Nothing else in the codebase notices either one. This does.
func TestErrMsgFormatsEveryErrorKey(t *testing.T) {
	localz := i18n.New("en")
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		t.Fatal(err)
	}

	// The same verbs check_i18n.sh compares across locales.
	verb := regexp.MustCompile(`%[sd]`)
	checked := 0

	for _, lang := range localz.Supported() {
		msgs := loadLocale(t, lang)
		for key, msg := range msgs {
			errKey, ok := strings.CutPrefix(key, "errors.")
			if !ok || !verb.MatchString(msg) {
				continue
			}
			checked++
			// Deliberately distinct values: whichever one the switch picks,
			// substituting the other is visible in the output.
			got := errMsg(localz, lang, errKey, 111, 222)
			if v := verb.FindString(got); v != "" {
				t.Errorf("errMsg(%q) [%s] left %q unfilled: %q — add a case to errMsg's switch",
					errKey, lang, v, got)
			}
			if strings.Contains(got, "%!") {
				t.Errorf("errMsg(%q) [%s] produced an fmt artefact: %q", errKey, lang, got)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no errors.* message with a format verb was checked — the walk is broken, not the locales")
	}
}

// loadLocale reads one embedded locale catalog. The Localizer deliberately
// exposes no way to enumerate keys (nothing in the app needs it); the test
// goes to the JSON instead of widening that API.
func loadLocale(t *testing.T, lang string) map[string]string {
	t.Helper()
	data, err := quickmock.LocalesFS.ReadFile("locales/" + lang + ".json")
	if err != nil {
		t.Fatalf("read locale %q: %v", lang, err)
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		t.Fatalf("parse locale %q: %v", lang, err)
	}
	return msgs
}
