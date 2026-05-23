package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

// Lang returns the POST /language handler.
//
// Accepts either form-encoded `lang=ru` or JSON `{"lang":"ru"}`. Sets the
// cookie and, for HTMX clients, returns the rendered header partial so the
// dropdown swaps in place. For plain clients returns 200 JSON.
//
// `secureCookie` is plumbed from main (true when BaseURL is https://).
func Lang(r *Renderer, secureCookie bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var lang string
		ct := req.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "application/json"):
			var body struct {
				Lang string `json:"lang"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, req, http.StatusBadRequest, "invalid_request", r)
				return
			}
			lang = body.Lang
		default:
			if err := req.ParseForm(); err != nil {
				writeError(w, req, http.StatusBadRequest, "invalid_request", r)
				return
			}
			lang = req.PostFormValue("lang")
		}

		if !r.localz.IsSupported(lang) {
			writeError(w, req, http.StatusBadRequest, "unknown_lang", r)
			return
		}

		i18n.SetLangCookie(w, lang, secureCookie)

		// HTMX request → tell the client to reload the current page so
		// the whole UI repaints in the new language, not just the header.
		// HX-Refresh is HTMX's built-in "do window.location.reload()".
		if req.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Refresh", "true")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Plain HTML form submit (no JS) → redirect back to where the
		// user came from, falling back to "/". Returning JSON here would
		// dump raw text onto the page.
		if strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			target := safeReferer(req.Header.Get("Referer"), req.Host)
			http.Redirect(w, req, target, http.StatusSeeOther)
			return
		}

		// Pure JSON client (e.g. future CLI) — keep machine-readable shape.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"lang": lang})
	}
}

// safeReferer returns a redirect target derived from the Referer header,
// restricted to same-origin paths. trustedHost is r.Host of the current
// request — the value the browser put in the URL bar, which (behind
// nginx with `proxy_set_header Host $host`) matches what same-origin
// Referers will carry. Cross-origin, protocol-relative, or unparsable
// Referers fall back to "/" so attacker.example cannot use POST /language
// as an open-redirect bounce.
func safeReferer(ref, trustedHost string) string {
	if ref == "" {
		return "/"
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "/"
	}
	// Absolute URLs are accepted only when they point at our own host;
	// browsers send the full URL for same-origin POSTs by default.
	if u.Host != "" && u.Host != trustedHost {
		return "/"
	}
	p := u.Path
	if p == "" {
		return "/"
	}
	// Path must be rooted and must NOT be protocol-relative (`//host/...`)
	// or use a backslash, which some browsers normalise into `/`.
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, `\`) {
		return "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}

// writeError centralizes JSON error responses for non-HTML routes.
func writeError(w http.ResponseWriter, req *http.Request, status int, code string, r *Renderer) {
	lang := i18n.LangFromContext(req.Context())
	if lang == "" {
		lang = r.localz.Fallback()
	}
	msg := r.localz.T(lang, "errors."+code)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}
