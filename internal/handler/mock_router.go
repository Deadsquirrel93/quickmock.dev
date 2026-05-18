package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// MockRouter handles the public surface — anything under /m/:slug.
//
// It is mounted on a router that does NOT include the UI i18n middleware
// (mock callers aren't browsers). It does include real-IP and rate-limit
// middlewares — see cmd/server/main.go.
type MockRouter struct {
	svc    *service.MockService
	logs   *service.LogWriter
	maxLog int
}

func NewMockRouter(svc *service.MockService, logs *service.LogWriter) *MockRouter {
	return &MockRouter{svc: svc, logs: logs, maxLog: 16 * 1024}
}

// ServeHTTP matches the slug, validates the method, sleeps the configured
// delay, replays the response, and asynchronously logs the incoming request.
func (h *MockRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	m, err := h.svc.Get(r.Context(), slug)
	if err != nil {
		writeNotFound(w)
		return
	}

	if !methodMatches(m.Method, r.Method) {
		w.Header().Set("Allow", allowFor(m.Method))
		http.Error(w, `{"error":{"code":"method_not_allowed","message":"Method not allowed for this mock"}}`,
			http.StatusMethodNotAllowed)
		return
	}

	// Read the body for logging up front — we drop it on the floor for
	// the actual response, but the inspector wants to see it.
	bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, int64(h.maxLog)+1))
	_ = r.Body.Close()

	// Submit log asynchronously. Drop on full queue, never block.
	h.logs.Submit(model.RequestLog{
		MockID:         m.ID,
		RequestMethod:  r.Method,
		RequestHeaders: flattenHeaders(r.Header),
		RequestBody:    string(bodyBytes),
		RequestIP:      mockmw.IPFromContext(r.Context()),
	})

	if m.ResponseDelayMS > 0 {
		select {
		case <-time.After(time.Duration(m.ResponseDelayMS) * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}

	for k, v := range m.ResponseHeaders {
		w.Header().Set(k, v)
	}
	if m.ContentType != "" {
		w.Header().Set("Content-Type", m.ContentType)
	}
	w.Header().Set("X-Mockapi-Slug", m.Slug)

	status := m.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(m.ResponseBody))
}

func methodMatches(mockMethod model.Method, requestMethod string) bool {
	if mockMethod == model.MethodANY {
		return true
	}
	return string(mockMethod) == requestMethod
}

func allowFor(m model.Method) string {
	if m == model.MethodANY {
		return "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD"
	}
	return string(m)
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		out[k] = v[0]
	}
	return out
}

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Mock not found"}}`))
}
