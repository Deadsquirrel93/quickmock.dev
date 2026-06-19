package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// API holds the JSON CRUD handlers used by the web UI and any future client.
type API struct {
	svc      *service.MockService
	logs     *repository.LogRepo
	mocks    *repository.MockRepo
	renderer *Renderer
	baseURL  string
}

func NewAPI(svc *service.MockService, logs *repository.LogRepo, mocks *repository.MockRepo, renderer *Renderer, baseURL string) *API {
	return &API{svc: svc, logs: logs, mocks: mocks, renderer: renderer, baseURL: baseURL}
}

// CreateMockRequest is the JSON shape accepted by POST /api/mocks.
type createMockRequest struct {
	Name               string               `json:"name"`
	Method             string               `json:"method"`
	ResponseBody       string               `json:"response_body"`
	ResponseStatus     int                  `json:"response_status"`
	ResponseHeaders    map[string]string    `json:"response_headers"`
	ResponseDelayMS    int                  `json:"response_delay_ms"`
	ResponseDelayMaxMS int                  `json:"response_delay_max_ms"`
	ErrorRatePct       int                  `json:"error_rate_pct"`
	ErrorResponse      *model.ResponseStep  `json:"error_response"`
	ResponseSequence   []model.ResponseStep `json:"response_sequence"`
	ContentType        string               `json:"content_type"`
	PathSuffix         string               `json:"path_suffix"`
	CORSEnabled        bool                 `json:"cors_enabled"`
	TTLSeconds         int                  `json:"ttl_seconds"`
}

func (req createMockRequest) toInput() model.MockInput {
	return model.MockInput{
		Name:               req.Name,
		Method:             model.Method(strings.ToUpper(req.Method)),
		ResponseBody:       req.ResponseBody,
		ResponseStatus:     req.ResponseStatus,
		ResponseHeaders:    req.ResponseHeaders,
		ResponseDelayMS:    req.ResponseDelayMS,
		ResponseDelayMaxMS: req.ResponseDelayMaxMS,
		ErrorRatePct:       req.ErrorRatePct,
		ErrorResponse:      req.ErrorResponse,
		SequenceSteps:      req.ResponseSequence,
		ContentType:        req.ContentType,
		PathSuffix:         req.PathSuffix,
		CORSEnabled:        req.CORSEnabled,
		TTL:                time.Duration(req.TTLSeconds) * time.Second,
	}
}

// Create handles POST /api/mocks.
func (a *API) Create(w http.ResponseWriter, r *http.Request) {
	var req createMockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", a.renderer)
		return
	}
	ip := mockmw.IPFromContext(r.Context())
	m, err := a.svc.Create(r.Context(), req.toInput(), ip)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.mockView(m))
}

// Get handles GET /api/mocks/:id.
func (a *API) Get(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "id")
	m, err := a.svc.Get(r.Context(), slug)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.mockView(m))
}

// Update handles PUT /api/mocks/:id.
func (a *API) Update(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "id")
	var req createMockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", a.renderer)
		return
	}
	m, err := a.svc.Update(r.Context(), slug, req.toInput())
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.mockView(m))
}

// Delete handles DELETE /api/mocks/:id.
func (a *API) Delete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "id")
	if err := a.svc.Delete(r.Context(), slug); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Logs handles GET /api/mocks/:id/logs.
func (a *API) Logs(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "id")
	m, err := a.svc.Get(r.Context(), slug)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 50
	}
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		since, _ = time.Parse(time.RFC3339, s)
	}
	logs, err := a.logs.ListByMockID(r.Context(), m.ID, limit, since)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", a.renderer)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logs":  logs,
		"total": len(logs),
	})
}

// ClearLogs handles DELETE /api/mocks/:id/logs.
func (a *API) ClearLogs(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "id")
	if err := a.svc.ClearLogs(r.Context(), slug); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ParseCurl handles POST /api/parse-curl — used by the UI to pre-fill a form.
func (a *API) ParseCurl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Curl string `json:"curl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", a.renderer)
		return
	}
	in, err := service.ParseCurl(body.Curl)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", a.renderer)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":           in.Method,
		"response_headers": in.ResponseHeaders,
		"response_body":    in.ResponseBody,
		"content_type":     in.ContentType,
	})
}

func (a *API) mockView(m *model.Mock) map[string]any {
	return map[string]any{
		"id":                    m.ID,
		"slug":                  m.Slug,
		"name":                  m.Name,
		"url":                   service.MockURL(m, a.baseURL),
		"method":                m.Method,
		"response_status":       m.ResponseStatus,
		"response_body":         m.ResponseBody,
		"response_headers":      m.ResponseHeaders,
		"response_delay_ms":     m.ResponseDelayMS,
		"response_delay_max_ms": m.ResponseDelayMaxMS,
		"error_rate_pct":        m.ErrorRatePct,
		"error_response":        m.ErrorResponse,
		"response_sequence":     m.SequenceSteps,
		"content_type":          m.ContentType,
		"path_suffix":           m.PathSuffix,
		"cors_enabled":          m.CORSEnabled,
		"expires_at":            m.ExpiresAt,
		"created_at":            m.CreatedAt,
		"request_count":         m.RequestCount,
		"last_request_at":       m.LastRequestAt,
	}
}

func (a *API) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", a.renderer)
	case errors.Is(err, service.ErrBodyTooLarge):
		writeError(w, r, http.StatusBadRequest, "body_too_large", a.renderer)
	case errors.Is(err, service.ErrMockLimitReached):
		writeError(w, r, http.StatusTooManyRequests, "mock_limit_reached", a.renderer)
	case isValidationErr(err):
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", a.renderer)
	default:
		writeError(w, r, http.StatusInternalServerError, "internal", a.renderer)
	}
}

func isValidationErr(err error) bool {
	var v *service.ValidationError
	return errors.As(err, &v) || errors.Is(err, service.ErrValidation)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
