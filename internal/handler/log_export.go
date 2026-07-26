package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// logsExportLimit is the number of rows fetched per export. It matches the
// DB-side trigger that already caps request_logs to each mock's 100 newest
// rows (migrations/001_init.sql), so this is effectively "export
// everything retained" rather than an arbitrary page size.
const logsExportLimit = 100

// LogsExport handles GET /mock/{slug}/logs/export — downloads a mock's
// captured requests as a JSON file. Unlike the mock-config export in
// export.go (slug-only, no token — the slug is that download's only
// credential and it never carries request data), this endpoint requires
// the mock's admin token via authorize()/AuthorizeSlug, the same rule
// every mutation already enforces.
//
// The sender IP of each captured request is intentionally KEPT here, unlike
// toExport() which strips IPs from the mock-config download. That download
// is meant to be shared/replayed (its slug is the only credential and could
// end up anywhere), so it must not carry any requester's IP. This one is
// gated by the admin token and exists specifically so the mock's owner can
// inspect real traffic (e.g. spot a misbehaving client) — stripping the IP
// would defeat that purpose.
func (u *UI) LogsExport(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	method, ok := logMethodFilter(r.URL.Query().Get("method"))
	if !ok {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", u.renderer)
		return
	}

	m, err := u.svc.AuthorizeSlug(r.Context(), slug, bearerToken(r))
	if err != nil {
		u.writeLogsExportError(w, r, err)
		return
	}

	logs, err := u.logs.ListByMockID(r.Context(), m.ID, logsExportLimit, time.Time{}, repository.LogFilter{Method: method})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", u.renderer)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+logsExportFilename(m.Slug)+`"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"logs":  logs,
		"total": len(logs),
	})
}

// logMethodFilter validates and normalizes the "method" query parameter.
// An empty value means no filter. A non-empty value that isn't one of
// model.AllMethods reports ok=false so the caller can answer 422 — this is
// a dedicated JSON download, not the lenient HTMX log partial (Task 7),
// which ignores bad input instead of erroring.
func logMethodFilter(raw string) (method string, ok bool) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return "", true
	}
	if !model.ValidMethod(raw) {
		return "", false
	}
	return raw, true
}

// logsExportFilename builds the download filename, including the slug so
// multiple exports don't collide in a browser's downloads folder.
func logsExportFilename(slug string) string {
	return "quickmock-" + slug + "-logs.json"
}

// writeLogsExportError maps the AuthorizeSlug error set to a response,
// mirroring API.writeServiceError's token branches for the errors this
// read-only path can actually return.
func (u *UI) writeLogsExportError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", u.renderer)
	case errors.Is(err, service.ErrTokenRequired):
		writeError(w, r, http.StatusUnauthorized, "admin_token_required", u.renderer)
	case errors.Is(err, service.ErrTokenInvalid):
		writeError(w, r, http.StatusForbidden, "admin_token_invalid", u.renderer)
	default:
		writeError(w, r, http.StatusInternalServerError, "internal", u.renderer)
	}
}
