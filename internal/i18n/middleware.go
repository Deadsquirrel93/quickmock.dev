package i18n

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// cookieName is the name of the cookie that stores the user's explicit
// language choice from the UI switcher.
const cookieName = "lang"

// ctxKey is a private type to prevent collisions with other packages.
type ctxKey struct{}

// WithLang stores lang in ctx.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// LangFromContext returns the language stored in ctx, or "" if none.
func LangFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}

// Middleware returns an http.Handler middleware that picks a language for
// each request and stores it in the request context.
//
// Detection order, highest priority first:
//  1. ?lang=<code> query param — makes hreflang / sitemap URLs actually
//     serve the language they advertise, so a bot following the canonical
//     ?lang=ru link sees Russian content (otherwise hreflang is a lie).
//  2. Cookie "lang" — user's explicit choice from the switcher.
//  3. Accept-Language header — first supported language in q-value order.
//  4. The Localizer's fallback language.
//
// The middleware is intended for UI routes only. Public mock endpoints
// (/m/:slug) should be mounted on a router that does not include it.
func (l *Localizer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := l.resolveLang(r)
		w.Header().Set("X-Lang", lang)
		next.ServeHTTP(w, r.WithContext(WithLang(r.Context(), lang)))
	})
}

func (l *Localizer) resolveLang(r *http.Request) string {
	// 1. ?lang=<code> query param.
	if q := r.URL.Query().Get("lang"); q != "" && l.IsSupported(q) {
		return q
	}
	// 2. Cookie.
	if c, err := r.Cookie(cookieName); err == nil {
		if l.IsSupported(c.Value) {
			return c.Value
		}
	}
	// 3. Accept-Language header.
	if h := r.Header.Get("Accept-Language"); h != "" {
		for _, code := range parseAcceptLanguage(h) {
			if l.IsSupported(code) {
				return code
			}
		}
	}
	// 4. Default.
	return l.fallback
}

// SetLangCookie writes the language cookie on the response. `secure` should
// be true when the site is served over HTTPS — the caller derives that from
// the configured BaseURL once at startup.
//
// Use this from the POST /language handler.
func SetLangCookie(w http.ResponseWriter, lang string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   31536000, // 1 year
		HttpOnly: false,    // not sensitive; allow JS to read for UI hints
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// parseAcceptLanguage returns the language tags from an Accept-Language
// header in descending q-value order. Tags with q<=0 are dropped, the rest
// are stable-sorted by q; an unparsable q keeps the default 1. Region
// subtags are stripped ("en-US" → "en") because our catalogs are keyed by
// base language.
func parseAcceptLanguage(h string) []string {
	type pair struct {
		tag string
		q   float64
	}
	var pairs []pair
	for _, part := range strings.Split(h, ",") {
		tag, params, _ := strings.Cut(part, ";")
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					q = f
				}
			}
		}
		tag, _, _ = strings.Cut(strings.TrimSpace(tag), "-")
		tag = strings.ToLower(tag)
		if q <= 0 || tag == "" || tag == "*" {
			continue
		}
		pairs = append(pairs, pair{tag, q})
	}
	slices.SortStableFunc(pairs, func(a, b pair) int { return cmp.Compare(b.q, a.q) })

	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if !slices.Contains(out, p.tag) {
			out = append(out, p.tag)
		}
	}
	return out
}
