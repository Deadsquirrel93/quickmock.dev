package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// UI groups the HTML-rendering handlers.
type UI struct {
	svc      *service.MockService
	logs     *repository.LogRepo
	stats    *service.StatsCache
	renderer *Renderer
	localz   *i18n.Localizer
	baseURL  string
	maxBody  int
	maxMocks int
}

func NewUI(svc *service.MockService, logs *repository.LogRepo, stats *service.StatsCache, renderer *Renderer, localz *i18n.Localizer, baseURL string, maxBody, maxMocks int) *UI {
	return &UI{svc: svc, logs: logs, stats: stats, renderer: renderer, localz: localz, baseURL: baseURL, maxBody: maxBody, maxMocks: maxMocks}
}

// Home renders GET /.
func (u *UI) Home(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}
	u.renderer.Render(w, r, "index", http.StatusOK, map[string]any{
		"Methods":   model.AllMethods,
		"MaxBody":   u.maxBody,
		"MaxMocks":  u.maxMocks,
		"MaxBodyKB": u.maxBody / 1024,
		"Stats":     u.stats.Snapshot(r.Context()),
		"JSONLD":    HomeJSONLD(u.localz, lang, u.baseURL, u.localz.Supported()),
	})
}

// CreateForm handles POST / — the HTML form submission.
//
// On HTMX requests we render the detail page partial; otherwise we redirect
// (Post/Redirect/Get) to /mock/:slug.
func (u *UI) CreateForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	in := readFormInput(r)
	ip := mockmw.IPFromContext(r.Context())
	m, err := u.svc.Create(r.Context(), in, ip)
	if err != nil {
		u.renderer.Render(w, r, "index", http.StatusOK, map[string]any{
			"Methods":   model.AllMethods,
			"MaxBody":   u.maxBody,
			"MaxBodyKB": u.maxBody / 1024,
			"MaxMocks":  u.maxMocks,
			"Stats":     u.stats.Snapshot(r.Context()),
			"Error":     errorKey(err),
			"Input":     in,
		})
		return
	}
	http.Redirect(w, r, "/mock/"+m.Slug, http.StatusSeeOther)
}

// Detail renders GET /mock/:slug — the management page.
func (u *UI) Detail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
	logs, _ := u.logs.ListByMockID(r.Context(), m.ID, 50, time.Time{})
	snippets := service.GenerateSnippets(m, u.baseURL)
	curl := service.CurlSnippet(m, u.baseURL)
	u.renderer.Render(w, r, "mock", http.StatusOK, map[string]any{
		"Mock":      m,
		"URL":       service.MockURL(m, u.baseURL),
		"Logs":      logs,
		"Snippets":  snippets,
		"Curl":      curl,
		"Methods":   model.AllMethods,
		"MaxBodyKB": u.maxBody / 1024,
	})
}

// LogsPartial returns just the log list — used by HTMX hx-trigger="every 2s".
func (u *UI) LogsPartial(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	logs, _ := u.logs.ListByMockID(r.Context(), m.ID, 50, time.Time{})
	u.renderer.Render(w, r, "partials_logs", http.StatusOK, map[string]any{
		"Mock": m,
		"Logs": logs,
	})
}

// SummaryPartial returns just the counter / last-request snippet — HTMX poll.
func (u *UI) SummaryPartial(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u.renderer.Render(w, r, "partials_summary", http.StatusOK, map[string]any{"Mock": m})
}

// MyMocks renders GET /my — the localStorage-driven mocks list.
//
// The page is a thin shell. A small Alpine.js bootstrap reads slugs from
// localStorage and asks /api/mocks/by-slugs to render the actual cards. To
// keep this simple for MVP, we render the empty shell and let the page do
// its work client-side.
func (u *UI) MyMocks(w http.ResponseWriter, r *http.Request) {
	u.renderer.Render(w, r, "list", http.StatusOK, nil)
}

// BySlugs returns metadata for many slugs at once. Slugs are passed as a
// comma-separated query param. Missing/expired slugs are silently skipped
// so the client can prune its localStorage.
func (u *UI) BySlugs(w http.ResponseWriter, r *http.Request) {
	slugs := r.URL.Query().Get("slugs")
	if slugs == "" {
		writeJSON(w, http.StatusOK, map[string]any{"mocks": []any{}})
		return
	}
	out := make([]map[string]any, 0)
	for _, s := range strings.Split(slugs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		m, err := u.svc.Get(r.Context(), s)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"slug":            m.Slug,
			"name":            m.Name,
			"method":          m.Method,
			"response_status": m.ResponseStatus,
			"request_count":   m.RequestCount,
			"created_at":      m.CreatedAt,
			"expires_at":      m.ExpiresAt,
			"path_suffix":     m.PathSuffix,
			"url":             service.MockURL(m, u.baseURL),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mocks": out})
}

// Share renders GET /share/:slug — a read-only public view.
func (u *UI) Share(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
	snippets := service.GenerateSnippets(m, u.baseURL)
	curl := service.CurlSnippet(m, u.baseURL)
	u.renderer.Render(w, r, "share", http.StatusOK, map[string]any{
		"Mock":     m,
		"URL":      service.MockURL(m, u.baseURL),
		"Snippets": snippets,
		"Curl":     curl,
	})
}

// NotFound is the catch-all for unknown UI routes.
func (u *UI) NotFound(w http.ResponseWriter, r *http.Request) {
	u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
}

func readFormInput(r *http.Request) model.MockInput {
	status, _ := strconv.Atoi(r.FormValue("response_status"))
	delay, _ := strconv.Atoi(r.FormValue("response_delay_ms"))
	ttlStr := r.FormValue("ttl")
	ttl, _ := time.ParseDuration(ttlStr)

	headers := map[string]string{}
	// Header rows arrive as parallel form fields `header_name[]` and
	// `header_value[]`. ParseForm exposes them via r.Form (a url.Values).
	names := r.Form["header_name[]"]
	values := r.Form["header_value[]"]
	for i, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		v := ""
		if i < len(values) {
			v = values[i]
		}
		headers[n] = v
	}

	return model.MockInput{
		Name:            r.FormValue("name"),
		Method:          model.Method(strings.ToUpper(r.FormValue("method"))),
		ResponseStatus:  status,
		ResponseBody:    r.FormValue("response_body"),
		ResponseHeaders: headers,
		ResponseDelayMS: delay,
		ContentType:     r.FormValue("content_type"),
		PathSuffix:      r.FormValue("path_suffix"),
		TTL:             ttl,
	}
}

func errorKey(err error) string {
	var v *service.ValidationError
	switch {
	case errors.As(err, &v):
		return "validation_failed"
	case errors.Is(err, service.ErrBodyTooLarge):
		return "body_too_large"
	case errors.Is(err, service.ErrMockLimitReached):
		return "mock_limit_reached"
	case errors.Is(err, service.ErrNotFound):
		return "not_found"
	default:
		return "internal"
	}
}

