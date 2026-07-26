package handler

import (
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
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
	seq    *repository.SeqCounter
	maxLog int
}

func NewMockRouter(svc *service.MockService, logs *service.LogWriter, seq *repository.SeqCounter) *MockRouter {
	return &MockRouter{svc: svc, logs: logs, seq: seq, maxLog: 16 * 1024}
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

	// Answer the browser's CORS preflight before method matching, so a
	// GET-only mock with CORS on still returns 204 instead of 405.
	if corsPreflight(w, m, r.Method) {
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

	served := pickVariant(m, rand.IntN(100), func() uint64 {
		return h.seq.Next(r.Context(), m.ID)
	})

	if d := effectiveDelay(m.ResponseDelayMS, m.ResponseDelayMaxMS); d > 0 {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}

	// User-controlled response headers go first; the protective headers
	// below overwrite anything the mock owner tries to set for them.
	// service.IsReservedResponseHeader is a second-layer filter — the
	// service already strips these at create/update time, but legacy rows
	// from before that filter widened could still carry weaponised headers.
	for k, v := range m.ResponseHeaders {
		if service.IsReservedResponseHeader(k) {
			continue
		}
		w.Header().Set(k, v)
	}
	// Sequence-step headers win over the mock's own per key; the ContentType
	// field below still wins over both — same precedence main headers have.
	for k, v := range served.Headers {
		if service.IsReservedResponseHeader(k) {
			continue
		}
		w.Header().Set(k, v)
	}
	if m.ContentType != "" {
		w.Header().Set("Content-Type", m.ContentType)
	}
	if m.CORSEnabled {
		setCORSHeaders(w)
	}
	w.Header().Set("X-Mockapi-Slug", m.Slug)
	w.Header().Set("X-Mockapi-Variant", served.Variant)

	// Defence against weaponising /m/:slug as a same-origin script host:
	// nosniff stops the browser from upgrading a text/* mock to text/html
	// based on content sniffing, and a strict sandbox CSP prevents JS,
	// forms, and popups from running when a browser navigates directly to
	// the mock URL. Machine clients (curl, fetch, http libs) ignore CSP,
	// so legitimate mock consumption is unaffected.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy",
		"sandbox; default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")

	status := served.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	// bodyBytes is capped at maxLog+1, so {{request.body*}} tokens see at
	// most the first 16 KB of the incoming body — same window as the
	// inspector.
	_, _ = w.Write([]byte(service.RenderResponseBodyForRequest(served.Body, &service.RequestData{
		Method: r.Method,
		Path:   r.URL.Path,
		IP:     mockmw.IPFromContext(r.Context()),
		Query:  r.URL.Query(),
		Header: r.Header,
		Body:   bodyBytes,
		// ":tpl" keeps the {{seq}} token's counter separate from the one
		// nextPos above uses to step through m.SequenceSteps — sharing a
		// key would make a {{seq}} token silently skip every other
		// sequence step by consuming a position on top of it.
		// +1 because SeqCounter.Next is 0-based — it exists to index into
		// m.SequenceSteps. {{seq}} is a user-facing hit counter instead, and
		// API.md documents it as starting at 1.
		Seq: func() uint64 { return h.seq.Next(r.Context(), m.ID+":tpl") + 1 },
	})))
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

// setCORSHeaders writes the fixed, permissive, credential-free CORS preset.
// Values are server-owned constants — the mock owner toggles cors_enabled but
// never picks them, which is why user-supplied Access-Control-* headers stay
// blocked by reservedHeaders in internal/service/mock.go. Omitting
// Allow-Credentials keeps `*` valid and means no cookies/credentials are ever
// exposed; the mock has none anyway.
func setCORSHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
	// `*` does not cover Authorization per the Fetch spec, so list it too —
	// frontend devs routinely send Bearer tokens.
	h.Set("Access-Control-Allow-Headers", "*, Authorization")
	h.Set("Access-Control-Max-Age", "600")
}

// corsPreflight answers a CORS preflight when the mock has CORS enabled,
// reporting whether it handled the request. Called before method matching so a
// GET-only mock still satisfies the browser's OPTIONS preflight.
func corsPreflight(w http.ResponseWriter, m *model.Mock, method string) bool {
	if !m.CORSEnabled || method != http.MethodOptions {
		return false
	}
	setCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
	return true
}

// secretRequestHeaders are request headers whose values likely contain a
// live credential (bearer token, API key, session cookie). We store a length
// marker instead of the value so the inspector still shows the header was
// present but the database never accumulates a haystack of real secrets —
// which would be an outsized breach blast radius for an open service.
var secretRequestHeaders = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	"x-auth-token":        {},
	"x-access-token":      {},
	"x-csrf-token":        {},
	"x-xsrf-token":        {},
}

// flattenHeaders folds http.Header into a flat map for JSONB storage.
// Keys are lowercased so downstream `request_headers->>'user-agent'`
// lookups in psql are predictable regardless of how the client cased
// them on the wire. Values for headers in secretRequestHeaders are
// replaced with a length marker (see comment above).
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		lk := strings.ToLower(k)
		val := v[0]
		if _, secret := secretRequestHeaders[lk]; secret && val != "" {
			val = fmt.Sprintf("[redacted len=%d]", len(val))
		}
		out[lk] = val
	}
	return out
}

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Mock not found"}}`))
}
