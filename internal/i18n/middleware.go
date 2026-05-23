package i18n

import (
	"context"
	"net/http"
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
//  1. Cookie "lang" — user's explicit choice.
//  2. Accept-Language header — first supported language in q-value order.
//  3. The Localizer's fallback language.
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
	// 1. Cookie.
	if c, err := r.Cookie(cookieName); err == nil {
		if l.IsSupported(c.Value) {
			return c.Value
		}
	}
	// 2. Accept-Language header.
	if h := r.Header.Get("Accept-Language"); h != "" {
		for _, code := range parseAcceptLanguage(h) {
			if l.IsSupported(code) {
				return code
			}
		}
	}
	// 3. Default.
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
// header in descending q-value order. Quality parsing is intentionally
// minimal: tags with q=0 are dropped, the rest are stable-sorted by q
// descending. Region subtags are stripped ("en-US" → "en") because our
// catalogs are keyed by base language.
func parseAcceptLanguage(h string) []string {
	type pair struct {
		tag string
		q   float64
	}
	var pairs []pair

	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			for _, p := range strings.Split(part[i+1:], ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "q=") {
					if v, err := parseQ(p[2:]); err == nil {
						q = v
					}
				}
			}
		}
		if q <= 0 {
			continue
		}
		// Strip region subtag: "en-US" → "en".
		if i := strings.Index(tag, "-"); i >= 0 {
			tag = tag[:i]
		}
		tag = strings.ToLower(tag)
		if tag == "" || tag == "*" {
			continue
		}
		pairs = append(pairs, pair{tag, q})
	}

	// Stable insertion-sort by q desc. The list is tiny (≤ ~5) in practice.
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j-1].q < pairs[j].q; j-- {
			pairs[j-1], pairs[j] = pairs[j], pairs[j-1]
		}
	}

	out := make([]string, 0, len(pairs))
	seen := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		if _, dup := seen[p.tag]; dup {
			continue
		}
		seen[p.tag] = struct{}{}
		out = append(out, p.tag)
	}
	return out
}

func parseQ(s string) (float64, error) {
	// Accept "0", "1", "0.x", "0.xx", "0.xxx". Anything weirder → default to 1.
	var (
		whole int
		frac  int
		div   = 1
	)
	if i := strings.Index(s, "."); i >= 0 {
		if i > 0 {
			if _, err := fmtAtoi(s[:i], &whole); err != nil {
				return 0, err
			}
		}
		f := s[i+1:]
		if len(f) > 3 {
			f = f[:3]
		}
		for range f {
			div *= 10
		}
		if f != "" {
			if _, err := fmtAtoi(f, &frac); err != nil {
				return 0, err
			}
		}
	} else {
		if _, err := fmtAtoi(s, &whole); err != nil {
			return 0, err
		}
	}
	return float64(whole) + float64(frac)/float64(div), nil
}

// fmtAtoi is a tiny ASCII-decimal parser. It avoids importing strconv just
// for one call, and keeps this file self-contained.
func fmtAtoi(s string, out *int) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, &parseErr{s}
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}

type parseErr struct{ s string }

func (e *parseErr) Error() string { return "i18n: bad number: " + e.s }
