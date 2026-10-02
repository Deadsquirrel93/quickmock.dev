package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postLang(t *testing.T, body, contentType string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	h := Lang(testUI(t).renderer, true)
	req := httptest.NewRequest(http.MethodPost, "https://example.test/language", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func langCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "lang" {
			return c
		}
	}
	return nil
}

func TestLangHTMXSetsCookieAndRefreshes(t *testing.T) {
	w := postLang(t, url.Values{"lang": {"ru"}}.Encode(), "application/x-www-form-urlencoded", map[string]string{"HX-Request": "true"})
	if w.Code != http.StatusNoContent || w.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("status = %d, HX-Refresh = %q", w.Code, w.Header().Get("HX-Refresh"))
	}
	if c := langCookie(w); c == nil || c.Value != "ru" || !c.Secure {
		t.Fatalf("lang cookie = %+v, want ru and Secure", c)
	}
}

func TestLangPlainFormRedirectsToSameOriginReferer(t *testing.T) {
	for _, tc := range []struct{ referer, want string }{
		{"https://example.test/guide?x=1", "/guide?x=1"},
		{"https://evil.example/phish", "/"},
		{"", "/"},
	} {
		w := postLang(t, "lang=ru", "application/x-www-form-urlencoded", map[string]string{"Referer": tc.referer})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tc.want {
			t.Errorf("Referer %q: %d → %q, want 303 → %q", tc.referer, w.Code, w.Header().Get("Location"), tc.want)
		}
	}
}

func TestLangRejectsUnknownLanguage(t *testing.T) {
	w := postLang(t, "lang=xx", "application/x-www-form-urlencoded", nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"unknown_lang"`) {
		t.Fatalf("got %d %q, want 400 unknown_lang", w.Code, w.Body.String())
	}
	if langCookie(w) != nil {
		t.Fatal("no cookie may be set for an unknown language")
	}
}

// The switcher is a plain form; a JSON body carries no form field.
func TestLangIgnoresJSONBody(t *testing.T) {
	if w := postLang(t, `{"lang":"ru"}`, "application/json", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
