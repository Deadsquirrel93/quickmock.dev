package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// fakeCreateStore is an in-memory service.MockStore. It exists so the
// success path of creating a mock can be exercised at all: both create
// handlers go through *service.MockService, which used to demand a concrete
// *repository.MockRepo and therefore a live Postgres.
type fakeCreateStore struct {
	created   []*model.Mock
	perIPUsed int
}

func (f *fakeCreateStore) Create(_ context.Context, m *model.Mock) error {
	f.created = append(f.created, m)
	return nil
}

func (f *fakeCreateStore) BySlug(_ context.Context, slug string) (*model.Mock, error) {
	for _, m := range f.created {
		if m.Slug == slug {
			cp := *m
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeCreateStore) Update(context.Context, *model.Mock) error  { return nil }
func (f *fakeCreateStore) DeleteBySlug(context.Context, string) error { return nil }
func (f *fakeCreateStore) SlugExists(context.Context, string) (bool, error) {
	return false, nil
}

func (f *fakeCreateStore) CountActiveByCreatorIP(context.Context, string) (int, error) {
	return f.perIPUsed, nil
}

// creatingUI is testUI plus a MockService over store, so CreateForm and
// TemplateCreate can run end to end.
func creatingUI(t *testing.T, store *fakeCreateStore, maxMocks int) *UI {
	t.Helper()
	u := testUI(t)
	u.svc = service.NewMockService(store, nil, nil, 512*1024, maxMocks, 168*time.Hour, 720*time.Hour, nil)
	u.maxBody = 512 * 1024
	u.maxMocks = maxMocks
	return u
}

// withClientIP runs h behind the real-IP middleware, so handlers see the
// same mockmw.IPFromContext value they see in production. The context key is
// unexported, so the middleware is the only way to set it from here.
func withClientIP(h http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	mockmw.RealIP("")(h).ServeHTTP(w, r)
}

func TestCreateFormSuccessPersistsAndRedirects(t *testing.T) {
	store := &fakeCreateStore{}
	u := creatingUI(t, store, 50)

	form := url.Values{
		"name":              {"orders api"},
		"method":            {"GET"},
		"response_status":   {"201"},
		"response_body":     {`{"ok":true}`},
		"content_type":      {"application/json"},
		"header_name[]":     {"X-Demo"},
		"header_value[]":    {"yes"},
		"response_delay_ms": {"0"},
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	withClientIP(u.CreateForm, w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", w.Code, w.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("mocks persisted = %d, want 1", len(store.created))
	}
	m := store.created[0]

	if m.Slug == "" {
		t.Error("created mock has no slug")
	}
	if m.Name != "orders api" || m.Method != "GET" || m.ResponseStatus != 201 {
		t.Errorf("form fields not carried through: %+v", m)
	}
	if m.ResponseBody != `{"ok":true}` {
		t.Errorf("response body = %q", m.ResponseBody)
	}
	if m.ResponseHeaders["X-Demo"] != "yes" {
		t.Errorf("response headers = %v, want the submitted X-Demo row", m.ResponseHeaders)
	}
	// The per-IP cap and the spam allowlist are both keyed on this; an empty
	// CreatorIP would silently disable them for every mock made from the form.
	if m.CreatorIP == "" {
		t.Error("creator IP not recorded")
	}
	if m.ExpiresAt == nil || !m.ExpiresAt.After(time.Now()) {
		t.Errorf("expiry = %v, want a future time", m.ExpiresAt)
	}
	// Only the hash is stored; the plaintext exists solely to be handed to
	// the browser once, in the redirect fragment.
	if m.AdminTokenHash == "" {
		t.Error("admin token hash not stored")
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/mock/"+m.Slug+"#token=") {
		t.Fatalf("Location = %q, want /mock/%s#token=…", loc, m.Slug)
	}
	if tok := strings.TrimPrefix(loc, "/mock/"+m.Slug+"#token="); tok == "" || tok == m.AdminTokenHash {
		t.Errorf("redirect must carry the one-time plaintext token, not the stored hash: %q", tok)
	}
}

func TestCreateFormPerIPLimitRendersBanner(t *testing.T) {
	store := &fakeCreateStore{perIPUsed: 50}
	u := creatingUI(t, store, 50)

	form := url.Values{"method": {"GET"}, "response_body": {"{}"}, "response_status": {"200"}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	withClientIP(u.CreateForm, w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (form re-rendered with a banner)", w.Code)
	}
	if len(store.created) != 0 {
		t.Fatalf("mock persisted despite the per-IP cap: %d", len(store.created))
	}
}

func TestTemplateCreateSuccessPersistsTemplateBody(t *testing.T) {
	store := &fakeCreateStore{}
	u := creatingUI(t, store, 50)

	r := withSlug(httptest.NewRequest(http.MethodPost, "/templates/openid-configuration/create", nil),
		"openid-configuration")
	w := httptest.NewRecorder()
	withClientIP(u.TemplateCreate, w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", w.Code, w.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("mocks persisted = %d, want 1", len(store.created))
	}
	m := store.created[0]

	in, ok := TemplateInput("openid-configuration")
	if !ok {
		t.Fatal("openid-configuration missing from the template registry")
	}
	if m.ResponseBody != in.ResponseBody {
		t.Errorf("stored body drifted from the registry:\n got %q\nwant %q", m.ResponseBody, in.ResponseBody)
	}
	if m.PathSuffix != in.PathSuffix {
		t.Errorf("path suffix = %q, want %q", m.PathSuffix, in.PathSuffix)
	}
	// The one-click CTA bypasses the form whose capture checkboxes default to
	// on, so TemplateCreate has to set them itself.
	if !m.CaptureBody || !m.CaptureIP {
		t.Errorf("capture defaults = body:%v ip:%v, want both true", m.CaptureBody, m.CaptureIP)
	}
	if !strings.HasPrefix(w.Header().Get("Location"), "/mock/"+m.Slug+"#token=") {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
}
