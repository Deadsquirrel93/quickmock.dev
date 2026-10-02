package i18n

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"
)

func testLocalizer(t *testing.T) *Localizer {
	t.Helper()
	l := New("en")
	err := l.LoadFS(fstest.MapFS{
		"locales/en.json": {Data: []byte(`{"hi":"Hello","n":"%d items","only_en":"EN"}`)},
		"locales/ru.json": {Data: []byte(`{"hi":"Привет","n":"%d штук"}`)},
		"locales/README":  {Data: []byte("not a locale")},
	}, "locales")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestParseAcceptLanguage(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7", []string{"ru", "en"}},
		{"en;q=0.5, ru", []string{"ru", "en"}},
		{"ru;q=0, en", []string{"en"}},
		{"*, en;q=0.1", []string{"en"}},
		{"en;q=0.8, ru;q=0.8, zh;q=0.8", []string{"en", "ru", "zh"}}, // stable on ties
		{"en-US, en-GB;q=0.9", []string{"en"}},                       // region stripped, deduped
		{"EN-us", []string{"en"}},
		{"en;q=abc, ru;q=0.5", []string{"en", "ru"}}, // unparsable q keeps the default 1
		{"ru;q=0.25, en;q=0.125", []string{"ru", "en"}},
		{"de; q=0.3 ;foo=bar, fr;q=1.0", []string{"fr", "de"}},
		{" , ;q=0.5, es", []string{"es"}},
	} {
		if got := parseAcceptLanguage(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("parseAcceptLanguage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadFS(t *testing.T) {
	l := testLocalizer(t)
	if got := l.Supported(); !slices.Equal(got, []string{"en", "ru"}) {
		t.Errorf("Supported() = %q", got)
	}
	if !l.IsSupported("ru") || l.IsSupported("README") || l.IsSupported("de") {
		t.Error("IsSupported disagrees with the loaded catalogs")
	}
	if err := New("de").LoadFS(fstest.MapFS{"l/en.json": {Data: []byte(`{}`)}}, "l"); err == nil {
		t.Error("want an error when the fallback language is missing")
	}
	if err := New("en").LoadFS(fstest.MapFS{"l/x.txt": {Data: []byte(``)}}, "l"); err == nil {
		t.Error("want an error when no locale files are found")
	}
	if err := New("en").LoadFS(fstest.MapFS{"l/en.json": {Data: []byte(`{`)}}, "l"); err == nil {
		t.Error("want an error on malformed JSON")
	}
	if New("").Fallback() != "en" {
		t.Error("empty fallback should default to en")
	}
}

func TestT(t *testing.T) {
	l := testLocalizer(t)
	for _, tc := range []struct {
		lang, key string
		args      []any
		want      string
	}{
		{"ru", "hi", nil, "Привет"},
		{"ru", "only_en", nil, "EN"}, // falls back to the default language
		{"de", "hi", nil, "Hello"},   // unknown language falls back too
		{"ru", "missing.key", nil, "missing.key"},
		{"ru", "n", []any{3}, "3 штук"},
		{"en", "hi", []any{3}, "Hello"}, // extra args ignored without a verb
	} {
		if got := l.T(tc.lang, tc.key, tc.args...); got != tc.want {
			t.Errorf("T(%q, %q) = %q, want %q", tc.lang, tc.key, got, tc.want)
		}
	}
}

func TestMiddlewareResolvesLang(t *testing.T) {
	l := testLocalizer(t)
	var got string
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LangFromContext(r.Context())
	}))
	for _, tc := range []struct {
		name, url, cookie, accept, want string
	}{
		{"query wins", "/?lang=ru", "en", "en", "ru"},
		{"unsupported query ignored", "/?lang=de", "ru", "", "ru"},
		{"cookie beats header", "/", "ru", "en", "ru"},
		{"bad cookie ignored", "/", "xx", "ru", "ru"},
		{"header", "/", "", "de, ru;q=0.5", "ru"},
		{"fallback", "/", "", "de", "en"},
	} {
		req := httptest.NewRequest("GET", tc.url, nil)
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: "lang", Value: tc.cookie})
		}
		if tc.accept != "" {
			req.Header.Set("Accept-Language", tc.accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if got != tc.want || w.Header().Get("X-Lang") != tc.want {
			t.Errorf("%s: lang = %q, X-Lang = %q, want %q", tc.name, got, w.Header().Get("X-Lang"), tc.want)
		}
	}
}
