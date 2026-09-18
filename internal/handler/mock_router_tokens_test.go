package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// servingRouter builds a real MockRouter over an in-memory store. This is
// only possible because service.MockStore is exported — MockRouter used to
// need a live Postgres to construct, which left the whole serve path without
// automated coverage.
//
// The LogWriter is never Start()ed: Submit only pushes onto its buffered
// queue, so nothing ever reaches the (absent) repository.
func servingRouter(t *testing.T, m *model.Mock, baseURL string) *MockRouter {
	t.Helper()
	store := &fakeCreateStore{created: []*model.Mock{m}}
	svc := service.NewMockService(store, nil, nil, 512*1024, 50, 168*time.Hour, 720*time.Hour, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	logs := service.NewLogWriter(nil, nil, nil, 16, logger, nil)
	return NewMockRouter(svc, logs, nil, logger, baseURL)
}

func servedBody(t *testing.T, m *model.Mock, baseURL, host string) string {
	t.Helper()
	h := servingRouter(t, m, baseURL)
	r := withSlug(httptest.NewRequest(http.MethodGet, "/m/"+m.Slug, nil), m.Slug)
	r.Host = host
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// The point of {{mock.url}}: a body that names the mock's own address, which
// cannot be written by hand because the slug only exists after creation.
func TestServeResolvesMockURLToken(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	m := &model.Mock{
		Slug:           "abc123",
		Method:         model.MethodGET,
		ResponseStatus: 200,
		ResponseBody:   `{"issuer":"{{mock.url}}","jwks_uri":"{{mock.url}}/.well-known/jwks.json"}`,
		ExpiresAt:      &expires,
	}
	got := servedBody(t, m, "https://quickmock.dev", "quickmock.dev")
	want := `{"issuer":"https://quickmock.dev/m/abc123","jwks_uri":"https://quickmock.dev/m/abc123/.well-known/jwks.json"}`
	if got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// {{mock.url}} reads the configured base URL, not the request's Host, so a
// forged Host cannot make a mock advertise an address it does not own.
// {{request.host}} is the echo token and does follow the header.
func TestServeMockURLIgnoresForgedHost(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	m := &model.Mock{
		Slug:           "abc123",
		Method:         model.MethodGET,
		ResponseStatus: 200,
		ResponseBody:   `{"self":"{{mock.url}}","host":"{{request.host}}"}`,
		ExpiresAt:      &expires,
	}
	got := servedBody(t, m, "https://quickmock.dev", "evil.example")
	want := `{"self":"https://quickmock.dev/m/abc123","host":"evil.example"}`
	if got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// A base URL with a trailing slash must not produce "…//m/slug".
func TestServeMockURLTrimsTrailingSlash(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	m := &model.Mock{
		Slug:           "abc123",
		Method:         model.MethodGET,
		ResponseStatus: 200,
		ResponseBody:   `{{mock.url}}`,
		ExpiresAt:      &expires,
	}
	if got := servedBody(t, m, "http://localhost:8090/", "localhost:8090"); got != "http://localhost:8090/m/abc123" {
		t.Fatalf("body = %q", got)
	}
}
