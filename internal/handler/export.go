package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// mockExport is the downloadable mock config. It is intentionally the exact
// JSON shape accepted by POST /api/mocks (see createMockRequest in api.go),
// so an exported file is directly replayable against the public API. It must
// never contain the slug (the slug is the mock's only credential), the ID,
// logs, counters, or the creator IP.
type mockExport struct {
	Name               string               `json:"name,omitempty"`
	Method             model.Method         `json:"method"`
	ResponseStatus     int                  `json:"response_status"`
	ResponseBody       string               `json:"response_body,omitempty"`
	ResponseHeaders    map[string]string    `json:"response_headers,omitempty"`
	ResponseDelayMS    int                  `json:"response_delay_ms,omitempty"`
	ResponseDelayMaxMS int                  `json:"response_delay_max_ms,omitempty"`
	ErrorRatePct       int                  `json:"error_rate_pct,omitempty"`
	ErrorResponse      *model.ResponseStep  `json:"error_response,omitempty"`
	ResponseSequence   []model.ResponseStep `json:"response_sequence,omitempty"`
	ResponseVariants   []model.NamedVariant `json:"response_variants,omitempty"`
	ResponseRules      []model.ResponseRule `json:"response_rules,omitempty"`
	Routes             []model.MockRoute    `json:"routes,omitempty"`
	ContentType        string               `json:"content_type,omitempty"`
	PathSuffix         string               `json:"path_suffix,omitempty"`
	CORSEnabled        bool                 `json:"cors_enabled,omitempty"`
	LogsPublic         bool                 `json:"logs_public,omitempty"`
	CaptureBody        bool                 `json:"capture_body"`
	CaptureIP          bool                 `json:"capture_ip"`
}

func toExport(m *model.Mock) mockExport {
	return mockExport{
		Name:               m.Name,
		Method:             m.Method,
		ResponseStatus:     m.ResponseStatus,
		ResponseBody:       m.ResponseBody,
		ResponseHeaders:    m.ResponseHeaders,
		ResponseDelayMS:    m.ResponseDelayMS,
		ResponseDelayMaxMS: m.ResponseDelayMaxMS,
		ErrorRatePct:       m.ErrorRatePct,
		ErrorResponse:      m.ErrorResponse,
		ResponseSequence:   m.SequenceSteps,
		ResponseVariants:   m.Variants,
		ResponseRules:      m.Rules,
		Routes:             m.Routes,
		ContentType:        m.ContentType,
		PathSuffix:         m.PathSuffix,
		CORSEnabled:        m.CORSEnabled,
		LogsPublic:         m.LogsPublic,
		CaptureBody:        m.CaptureBody,
		CaptureIP:          m.CaptureIP,
	}
}

var exportNameStrip = regexp.MustCompile(`[^a-z0-9]+`)

// safeExportName builds the download filename from the mock's display name.
// NOT from the slug — a shared export file must not leak the credential.
func safeExportName(name string) string {
	s := exportNameStrip.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "mock"
	}
	return s
}

// Export handles GET /mock/:slug/export — downloads the mock config as JSON.
func (u *UI) Export(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	m, err := u.svc.Get(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="quickmock-`+safeExportName(m.Name)+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(toExport(m))
}
