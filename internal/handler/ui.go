package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
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
	"github.com/Deadsquirrel93/quickmock.dev/internal/sse"
)

// writeLimiter is the sliver of repository.RateLimiter the UI needs: one
// quota check per state-changing request. Kept narrow so the handlers can be
// tested against a fake instead of a live Redis.
type writeLimiter interface {
	Allow(ctx context.Context, key string) (repository.Decision, error)
}

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
	broker   *sse.Broker
	streams  *sse.StreamLimiter
	writes   writeLimiter
}

func NewUI(svc *service.MockService, logs *repository.LogRepo, stats *service.StatsCache, renderer *Renderer, localz *i18n.Localizer, baseURL string, maxBody, maxMocks int, broker *sse.Broker, streams *sse.StreamLimiter, writes writeLimiter) *UI {
	return &UI{svc: svc, logs: logs, stats: stats, renderer: renderer, localz: localz, baseURL: baseURL, maxBody: maxBody, maxMocks: maxMocks, broker: broker, streams: streams, writes: writes}
}

// writeAllowed consumes one unit of the state-changing-UI quota for the
// caller's IP. A limiter error fails open, the same call mockmw.RateLimit
// makes: Redis being unreachable must not stop people from creating mocks.
func (u *UI) writeAllowed(r *http.Request) bool {
	if u.writes == nil {
		return true
	}
	ip := mockmw.IPFromContext(r.Context())
	dec, err := u.writes.Allow(r.Context(), "rl:uiwrite:"+ip)
	if err != nil {
		return true
	}
	return dec.Allowed
}

// homeData builds the render data the index template needs, shared by Home
// and by every CreateForm branch that has to re-render the form instead of
// redirecting — so an error or a throttled submit comes back on the same
// page the user submitted, not a thinner copy of it. extra is merged last.
func (u *UI) homeData(r *http.Request, extra map[string]any) map[string]any {
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}
	// u.stats is nil in the render-only test fixture (no DB behind it); the
	// real server always wires one up via NewUI.
	stats := map[string]int64{}
	if u.stats != nil {
		stats = u.stats.Snapshot(r.Context())
	}
	data := map[string]any{
		"Methods":   model.AllMethods,
		"MaxBody":   u.maxBody,
		"MaxMocks":  u.maxMocks,
		"MaxBodyKB": u.maxBody / 1024,
		"Stats":     stats,
		"JSONLD":    HomeJSONLD(u.localz, lang, u.baseURL, u.localz.Supported()),
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// Home renders GET /.
func (u *UI) Home(w http.ResponseWriter, r *http.Request) {
	data := u.homeData(r, nil)
	// A "Create this mock" CTA on a /guide/<slug> page links here with
	// ?prefill=<slug>; hand the create form that case's config (the registry
	// is the single source of truth) so the Alpine form can populate itself.
	// A /templates/<slug> gallery card offers the same CTA via
	// ?prefill_template=<slug>; when both are present the guide wins.
	if prefill, ok := guidePrefill(r.URL.Query().Get("prefill")); ok {
		data["Prefill"] = prefill
	} else if prefill, ok := templatePrefill(r.URL.Query().Get("prefill_template")); ok {
		data["Prefill"] = prefill
	}
	u.renderer.Render(w, r, "index", http.StatusOK, data)
}

// guidePrefill returns a use-case's create body as inline JS for the home form
// to apply, when slug names a known guide. The body is in-repo trusted content.
func guidePrefill(slug string) (template.JS, bool) {
	if slug == "" {
		return "", false
	}
	if c, ok := UseCaseBySlug(slug); ok {
		return template.JS(c.CreateBody), true
	}
	return "", false
}

// templatePrefill returns a gallery template's create body as inline JS for
// the home form to apply, when slug names a known /templates/<slug> entry.
// The body is in-repo trusted content, same as guidePrefill.
func templatePrefill(slug string) (template.JS, bool) {
	if slug == "" {
		return "", false
	}
	if tpl, ok := TemplateBySlug(slug); ok {
		return template.JS(tpl.CreateBody), true
	}
	return "", false
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
	if !u.writeAllowed(r) {
		u.renderer.Render(w, r, "index", http.StatusOK, u.homeData(r, map[string]any{
			"Error": "rate_limit",
			"Input": readFormInput(r),
		}))
		return
	}
	in, parseErr := readFormInputChecked(r)
	if parseErr != nil {
		u.renderer.Render(w, r, "index", http.StatusOK, u.homeData(r, map[string]any{
			"Error": "validation_failed",
			"Input": readFormInput(r),
		}))
		return
	}
	ip := mockmw.IPFromContext(r.Context())
	m, err := u.svc.Create(r.Context(), in, ip)
	if err != nil {
		u.renderer.Render(w, r, "index", http.StatusOK, u.homeData(r, map[string]any{
			"Error": errorKey(err),
			"Input": in,
		}))
		return
	}
	http.Redirect(w, r, mockRedirectLocation(m), http.StatusSeeOther)
}

// mockRedirectLocation builds the Post/Redirect/Get target for a freshly
// created mock. The one-time admin token — when the mock has one — rides in
// the URL fragment: fragments never leave the browser (no server access
// logs, no Referer leakage), so this is the only safe channel to hand it to
// the detail page for its one-time display.
func mockRedirectLocation(m *model.Mock) string {
	loc := "/mock/" + m.Slug
	if m.AdminToken != "" {
		loc += "#token=" + m.AdminToken
	}
	return loc
}

// Detail renders GET /mock/:slug — the management page.
func (u *UI) Detail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
	inspectorAuthorized := m.LogsPublic || u.inspectorAuthorized(r, m)
	var logs []model.RequestLog
	if inspectorAuthorized {
		logs, _ = u.logs.ListByMockID(r.Context(), m.ID, 50, time.Time{}, repository.LogFilter{})
	}
	snippets := service.GenerateSnippets(m, u.baseURL)
	curl := service.CurlSnippet(m, u.baseURL)
	u.renderer.Render(w, r, "mock", http.StatusOK, map[string]any{
		"Mock":            m,
		"URL":             service.MockURL(m, u.baseURL),
		"Logs":            logs,
		"Snippets":        snippets,
		"Curl":            curl,
		"Methods":         model.AllMethods,
		"MaxBodyKB":       u.maxBody / 1024,
		"InspectorLocked": !inspectorAuthorized,
	})
}

// MockSession exchanges the admin token already kept in browser localStorage
// for a short, HttpOnly, path-scoped cookie. EventSource cannot attach an
// Authorization header, so this lets the private live inspector authenticate
// without putting its token in a query string or server logs.
func (u *UI) MockSession(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.AuthorizeSlug(r.Context(), slug, bearerToken(r))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "qm_inspector", Value: bearerToken(r), Path: "/mock/" + m.Slug,
		HttpOnly: true, Secure: strings.HasPrefix(strings.ToLower(u.baseURL), "https://"),
		SameSite: http.SameSiteStrictMode, MaxAge: 60 * 60 * 24 * 30,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) inspectorAuthorized(r *http.Request, m *model.Mock) bool {
	if m.LogsPublic {
		return true
	}
	token := bearerToken(r)
	if token == "" {
		if cookie, err := r.Cookie("qm_inspector"); err == nil {
			token = cookie.Value
		}
	}
	_, err := u.svc.AuthorizeSlug(r.Context(), m.Slug, token)
	return err == nil
}

// logFilterMethods lists the concrete HTTP methods a captured request can
// carry. Unlike model.AllMethods (which also lists ANY — a mock's own
// wildcard match rule, never a caller's actual verb), this drives the
// inspector's method filter dropdown: offering ANY there would always
// return zero rows, since no logged request ever has that method.
var logFilterMethods = []model.Method{
	model.MethodGET, model.MethodPOST, model.MethodPUT, model.MethodPATCH, model.MethodDELETE,
}

// logsPartialMethod normalizes the "method" query parameter for the HTMX
// log partial. Unlike logMethodFilter (log_export.go), which answers 422 on
// garbage input, this is a lenient, repeatedly-polled fragment: an
// unrecognized value — including "ANY", see logFilterMethods — is treated
// the same as "no filter" instead of failing the request.
func logsPartialMethod(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	for _, m := range logFilterMethods {
		if string(m) == raw {
			return raw
		}
	}
	return ""
}

// LogsPartial returns just the log list — used by HTMX hx-trigger="every 2s".
func (u *UI) LogsPartial(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !u.inspectorAuthorized(r, m) {
		u.renderer.Render(w, r, "partials_logs", http.StatusOK, map[string]any{
			"Mock": m, "InspectorLocked": true,
		})
		return
	}
	method := logsPartialMethod(r.URL.Query().Get("method"))
	logs, _ := u.logs.ListByMockID(r.Context(), m.ID, 50, time.Time{}, repository.LogFilter{Method: method})
	u.renderer.Render(w, r, "partials_logs", http.StatusOK, map[string]any{
		"Mock":          m,
		"Logs":          logs,
		"Method":        method,
		"FilterMethods": logFilterMethods,
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

// Changelog renders GET /changelog — a curated technical changelog. The
// page is fully static; the entries live in the template, hand-picked from
// the git history.
func (u *UI) Changelog(w http.ResponseWriter, r *http.Request) {
	u.renderer.Render(w, r, "changelog", http.StatusOK, nil)
}

// Guide renders GET /guide — the use-case index.
func (u *UI) Guide(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}
	u.renderer.Render(w, r, "guide", http.StatusOK, map[string]any{
		"Cases":           UseCases,
		"MetaTitle":       u.localz.T(lang, "guide.title") + " — " + u.localz.T(lang, "app.name"),
		"MetaDescription": u.localz.T(lang, "guide.meta_description"),
	})
}

// GuideCase renders GET /guide/:slug — one use-case landing page.
func (u *UI) GuideCase(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, ok := UseCaseBySlug(slug)
	if !ok {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}
	title := u.localz.T(lang, c.KeyPrefix+".title")
	u.renderer.Render(w, r, "guide_case", http.StatusOK, map[string]any{
		"Case":            c,
		"CreateCurl":      guideCreateCurl(u.baseURL, c),
		"CallCurl":        guideCallCurl(u.baseURL, c),
		"MetaTitle":       title + " — " + u.localz.T(lang, "app.name"),
		"MetaDescription": u.localz.T(lang, c.KeyPrefix+".summary"),
		"JSONLD":          GuideCaseJSONLD(u.localz, lang, u.baseURL, c),
	})
}

// guideCreateCurl renders the copy-paste create command for a case.
func guideCreateCurl(baseURL string, c UseCase) string {
	return createCurl(baseURL, c.CreateBody)
}

// guideCallCurl renders the command that calls the created mock. The slug is a
// placeholder the reader replaces with the slug from the create response.
func guideCallCurl(baseURL string, c UseCase) string {
	return callCurl(baseURL, c.CallVerb, c.CallHeader, c.CallData, "")
}

// createCurl renders the copy-paste command that creates a mock from
// createBody — shared by /guide/<slug> and /templates/<slug> case pages.
func createCurl(baseURL, createBody string) string {
	return fmt.Sprintf("curl -X POST %s/api/mocks \\\n  -H 'Content-Type: application/json' \\\n  -d '%s'",
		strings.TrimRight(baseURL, "/"), createBody)
}

// callCurl renders the command that calls a created mock. The slug is a
// placeholder the reader replaces with the slug from the create response.
// header and data are optional ("" if unused); pathSuffix is the mock's
// configured path suffix ("" if none), appended after the slug the same way
// service.MockURL does.
func callCurl(baseURL, verb, header, data, pathSuffix string) string {
	base := strings.TrimRight(baseURL, "/")
	parts := []string{"curl"}
	if verb != "GET" {
		parts = append(parts, "-X "+verb)
	}
	if header != "" {
		parts = append(parts, "-H '"+header+"'")
	}
	if data != "" {
		parts = append(parts, "-H 'Content-Type: application/json'", "-d '"+data+"'")
	}
	url := base + "/m/<slug>"
	if pathSuffix != "" {
		url += "/" + pathSuffix
	}
	parts = append(parts, url)
	return strings.Join(parts, " ")
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
		out = append(out, u.bySlugView(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"mocks": out})
}

// bySlugView is the per-mock shape returned by BySlugs for the /my list
// cards. It is deliberately a small, hand-picked subset of fields — never
// admin_token or admin_token_hash, since /my is driven by a plain slug list
// from localStorage, not an authenticated call.
func (u *UI) bySlugView(m *model.Mock) map[string]any {
	return map[string]any{
		"slug":            m.Slug,
		"name":            m.Name,
		"method":          m.Method,
		"response_status": m.ResponseStatus,
		"request_count":   m.RequestCount,
		"created_at":      m.CreatedAt,
		"expires_at":      m.ExpiresAt,
		"path_suffix":     m.PathSuffix,
		"url":             service.MockURL(m, u.baseURL),
	}
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
	in, _ := readFormInputChecked(r)
	return in
}

func readFormInputChecked(r *http.Request) (model.MockInput, error) {
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

	delayMax, _ := strconv.Atoi(r.FormValue("response_delay_max_ms"))
	errRate, _ := strconv.Atoi(r.FormValue("error_rate_pct"))

	var errResp *model.ResponseStep
	if errRate > 0 {
		errStatus, _ := strconv.Atoi(r.FormValue("error_status"))
		errResp = &model.ResponseStep{Status: errStatus, Body: r.FormValue("error_body")}
	}

	// Sequence steps arrive as parallel arrays, same trick as headers.
	// Rows the user added but left fully empty are dropped.
	var steps []model.ResponseStep
	stStatus := r.Form["seq_status[]"]
	stBody := r.Form["seq_body[]"]
	stHeaders := r.Form["seq_headers[]"]
	for i := range stStatus {
		status, _ := strconv.Atoi(stStatus[i])
		body, hdrs := "", ""
		if i < len(stBody) {
			body = stBody[i]
		}
		if i < len(stHeaders) {
			hdrs = stHeaders[i]
		}
		if status == 0 && strings.TrimSpace(body) == "" && strings.TrimSpace(hdrs) == "" {
			continue
		}
		steps = append(steps, model.ResponseStep{
			Status:  status,
			Body:    body,
			Headers: parseHeaderLines(hdrs),
		})
	}

	var variants []model.NamedVariant
	var rules []model.ResponseRule
	var routes []model.MockRoute
	for _, item := range []struct {
		raw string
		to  any
	}{
		{r.FormValue("response_variants_json"), &variants},
		{r.FormValue("response_rules_json"), &rules},
		{r.FormValue("routes_json"), &routes},
	} {
		if strings.TrimSpace(item.raw) != "" {
			if err := json.Unmarshal([]byte(item.raw), item.to); err != nil {
				return model.MockInput{}, err
			}
		}
	}

	return model.MockInput{
		Name:               r.FormValue("name"),
		Method:             model.Method(strings.ToUpper(r.FormValue("method"))),
		ResponseStatus:     status,
		ResponseBody:       r.FormValue("response_body"),
		ResponseHeaders:    headers,
		ResponseDelayMS:    delay,
		ResponseDelayMaxMS: delayMax,
		ErrorRatePct:       errRate,
		ErrorResponse:      errResp,
		SequenceSteps:      steps,
		Variants:           variants,
		Rules:              rules,
		Routes:             routes,
		ContentType:        r.FormValue("content_type"),
		PathSuffix:         r.FormValue("path_suffix"),
		CORSEnabled:        r.FormValue("cors_enabled") == "on",
		LogsPublic:         r.FormValue("logs_public") == "on",
		CaptureBody:        r.FormValue("capture_body") == "on",
		CaptureIP:          r.FormValue("capture_ip") == "on",
		TTL:                ttl,
	}, nil
}

// parseHeaderLines turns a "Name: value" per-line textarea into a header
// map. Lines without a colon or without a name are skipped; nil is returned
// for an effectively empty input so the steps stay clean in storage.
func parseHeaderLines(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		name, value, ok := strings.Cut(strings.TrimRight(line, "\r"), ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	case errors.Is(err, service.ErrSpamBlocked):
		return "spam_blocked"
	default:
		return "internal"
	}
}
