// Package handler holds the HTTP handlers — thin glue between the router
// and the service / template layers.
package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// LastUpdated is the human-visible "last meaningful content change" date for
// the home page. Bump it when the on-page content (copy, FAQ, sections,
// schema) changes in a way that should refresh SERP / AI freshness signals;
// don't bump it for tiny CSS or wording tweaks.
const LastUpdated = "2026-08-09"

// Renderer compiles every template once at startup and renders pages with a
// per-request i18n FuncMap injected.
type Renderer struct {
	pages    map[string]*template.Template
	localz   *i18n.Localizer
	logger   *slog.Logger
	baseURL  string
	assetVer string
	defaults map[string]any
}

// assetVersion returns a short content hash of the embedded static asset tree
// (fsys/static). It changes whenever any static file's path or content
// changes, so it can be appended to asset URLs (?v=…) to bust stale
// client/CDN caches the moment a new build ships — while still letting the
// files themselves be cached for a long time. Non-static files (templates,
// locales) are ignored so an unrelated edit doesn't force a re-download.
func assetVersion(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		// Fold the path in too so a rename alone changes the version.
		io.WriteString(h, path)
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// NewRenderer parses all top-level pages from fsys/templates, each combined
// with every partial under templates/partials so that {{ template "header" }}
// just works.
func NewRenderer(fsys fs.FS, localz *i18n.Localizer, logger *slog.Logger, baseURL string) (*Renderer, error) {
	assetVer, err := assetVersion(fsys)
	if err != nil {
		return nil, fmt.Errorf("asset version: %w", err)
	}

	r := &Renderer{
		pages:    make(map[string]*template.Template),
		localz:   localz,
		logger:   logger,
		baseURL:  baseURL,
		assetVer: assetVer,
		defaults: map[string]any{"BaseURL": baseURL},
	}

	partials, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	pages, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}

	// base.html lives among the pages but is not a renderable page itself —
	// it's the layout every full page extends. Pull it out so each page tree
	// includes it plus every partial.
	var basePath string
	var pageFiles []string
	for _, p := range pages {
		if strings.HasSuffix(p, "/base.html") {
			basePath = p
			continue
		}
		pageFiles = append(pageFiles, p)
	}

	funcs := r.baseFuncMap("en") // overridden per request; we just need a parse-time placeholder

	for _, p := range pageFiles {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".html")
		isPartial := strings.HasPrefix(name, "partials_") || name == "header_response"
		// Order matters: the FIRST file in ParseFS becomes the tree's root
		// template (named after its filename). For full pages we want
		// base.html as the root — otherwise html/template complains that
		// the page is "incomplete" because it contains only `{{ define }}`
		// blocks. HTMX-only partials have no layout, so they own the root.
		var files []string
		if basePath != "" && !isPartial {
			files = append(files, basePath, p)
		} else {
			files = append(files, p)
		}
		files = append(files, partials...)
		// Start from an empty named template carrying just the FuncMap so
		// ParseFS attaches every file's `define` blocks beside it.
		t, err := template.New("root").Funcs(funcs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

// Render writes the named page to w. `data` may be nil. The "base.html"
// template, if present, is used as the entry point; otherwise the page
// renders standalone.
func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, name string, status int, data map[string]any) {
	t, ok := r.pages[name]
	if !ok {
		r.logger.Error("unknown template", slog.String("name", name))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	lang := i18n.LangFromContext(req.Context())
	if lang == "" {
		lang = r.localz.Fallback()
	}

	full := map[string]any{
		"Lang":         lang,
		"Languages":    r.localz.Supported(),
		"BaseURL":      r.baseURL,
		"Path":         req.URL.Path,
		"DefaultLang":  r.localz.Fallback(),
		"LastUpdated":  LastUpdated,
		"AssetVersion": r.assetVer,
	}
	for k, v := range data {
		full[k] = v
	}

	// Clone the parsed template so we can swap in a request-scoped FuncMap
	// that closes over the resolved language.
	clone, err := t.Clone()
	if err != nil {
		r.logger.Error("clone template", slog.Any("err", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	clone = clone.Funcs(r.baseFuncMap(lang))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	// Pages prefixed with "partials_" are HTMX fragments — they render
	// themselves, not the full base layout. Same for header_response,
	// which the language switcher uses to re-paint just the header.
	entry := "base"
	if strings.HasPrefix(name, "partials_") || name == "header_response" {
		entry = name
	}
	if err := clone.ExecuteTemplate(w, entry, full); err != nil {
		// Once headers are flushed there's not much we can do.
		r.logger.Error("template execute", slog.String("page", name), slog.Any("err", err))
	}
}

// errMsg renders the localized text for an error key from errorKey (ui.go),
// passing along exactly the numeric argument that key's locale string has a
// placeholder for. errors.body_too_large has one %d for the body size limit
// (KB); errors.mock_limit_reached has one %d for the active-mocks limit;
// every other key takes no arguments. Localizer.T (i18n.go) only swallows
// extra args when the message has no "%" at all, so handing it two args
// when the message format has exactly one produces a literal
// "%!(EXTRA ...)" tail — or, since both callers' args are ints, silently
// substitutes the wrong number instead. Keeping that per-key mapping here,
// once, is the point: every template that shows this banner calls this
// instead of re-deciding which limit its error key wants.
func errMsg(localz *i18n.Localizer, lang, errKey string, maxBodyKB, maxMocks int) string {
	key := "errors." + errKey
	switch errKey {
	case "body_too_large":
		return localz.T(lang, key, maxBodyKB)
	case "mock_limit_reached":
		return localz.T(lang, key, maxMocks)
	default:
		return localz.T(lang, key)
	}
}

func (r *Renderer) baseFuncMap(lang string) template.FuncMap {
	return template.FuncMap{
		"t": func(key string, args ...any) string {
			return r.localz.T(lang, key, args...)
		},
		"errMsg": func(errKey string, maxBodyKB, maxMocks int) string {
			return errMsg(r.localz, lang, errKey, maxBodyKB, maxMocks)
		},
		"currentLang": func() string { return lang },
		"langName": func(code string) string {
			return r.localz.T(lang, "lang."+code)
		},
		"langOgLocale": func(code string) string {
			switch code {
			case "en":
				return "en_US"
			case "ru":
				return "ru_RU"
			case "kk":
				return "kk_KZ"
			case "ky":
				return "ky_KG"
			case "uz":
				return "uz_UZ"
			case "ar":
				return "ar_AR"
			default:
				return code
			}
		},
		"sortedHeaders": func(h map[string]string) [][2]string {
			keys := make([]string, 0, len(h))
			for k := range h {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out := make([][2]string, 0, len(keys))
			for _, k := range keys {
				out = append(out, [2]string{k, h[k]})
			}
			return out
		},
		"fmtTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04:05 MST")
		},
		"safeHTML": func(s string) template.HTML {
			// Trusted: only used for static, in-repo locale strings that
			// already contain markup (e.g. <code>, <strong>) — never for
			// user input.
			return template.HTML(s)
		},
		"tHTML": func(key string, args ...any) template.HTML {
			return template.HTML(r.localz.T(lang, key, args...))
		},
		"highlightJSON": service.HighlightJSON,
		"isJSONContentType": func(ct string) bool {
			return strings.Contains(strings.ToLower(ct), "json")
		},
		"humanCount": func(n int64) string {
			// Comma-separated for honesty (1,234 not 1.2K). Numbers stay
			// short for many years; abbreviation hides early-stage growth.
			if n < 0 {
				return "0"
			}
			s := fmt.Sprintf("%d", n)
			// Insert commas every 3 digits from the right.
			var out strings.Builder
			for i, c := range s {
				if i > 0 && (len(s)-i)%3 == 0 {
					out.WriteByte(',')
				}
				out.WriteRune(c)
			}
			return out.String()
		},
		"statValue": func(stats map[string]int64, key string) int64 {
			if stats == nil {
				return 0
			}
			return stats[key]
		},
		"isHTMX": func(req *http.Request) bool {
			return req != nil && req.Header.Get("HX-Request") == "true"
		},
		"errIs": func(err error, target error) bool {
			return errors.Is(err, target)
		},
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}
}
