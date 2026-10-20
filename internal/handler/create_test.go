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
	for _, c := range f.created {
		if c.Slug == m.Slug {
			return repository.ErrSlugConflict
		}
	}
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
	u.svc = service.NewMockService(store, nil, nil, 512*1024, maxMocks, 168*time.Hour, 720*time.Hour, nil, nil)
	u.maxBody = 512 * 1024
	u.maxMocks = maxMocks
	return u
}

// withClientIP runs h behind the real-IP middleware, so handlers see the
// same bare-IP r.RemoteAddr they see in production.
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

func postCreateForm(u *UI, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	withClientIP(u.CreateForm, w, r)
	return w
}

func TestCreateFormCustomSlug(t *testing.T) {
	base := func(slug string) url.Values {
		return url.Values{"method": {"GET"}, "response_body": {"{}"}, "response_status": {"200"}, "slug": {slug}}
	}

	t.Run("custom slug is used and redirected to", func(t *testing.T) {
		store := &fakeCreateStore{}
		w := postCreateForm(creatingUI(t, store, 50), base("my-form-mock"))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303; body: %s", w.Code, w.Body.String())
		}
		if len(store.created) != 1 || store.created[0].Slug != "my-form-mock" {
			t.Fatalf("persisted = %+v, want one mock with slug my-form-mock", store.created)
		}
		if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/mock/my-form-mock#token=") {
			t.Errorf("Location = %q", loc)
		}
	})

	t.Run("input is normalized", func(t *testing.T) {
		store := &fakeCreateStore{}
		w := postCreateForm(creatingUI(t, store, 50), base("  My-Form-Mock "))
		if w.Code != http.StatusSeeOther || len(store.created) != 1 || store.created[0].Slug != "my-form-mock" {
			t.Fatalf("status = %d, persisted = %+v", w.Code, store.created)
		}
	})

	t.Run("blank slug falls back to a generated one", func(t *testing.T) {
		store := &fakeCreateStore{}
		w := postCreateForm(creatingUI(t, store, 50), base("   "))
		if w.Code != http.StatusSeeOther || len(store.created) != 1 || len(store.created[0].Slug) != 12 {
			t.Fatalf("status = %d, persisted = %+v", w.Code, store.created)
		}
	})

	t.Run("taken slug re-renders the form with the error", func(t *testing.T) {
		store := &fakeCreateStore{created: []*model.Mock{{Slug: "my-form-mock"}}}
		w := postCreateForm(creatingUI(t, store, 50), base("my-form-mock"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (form re-rendered)", w.Code)
		}
		if !strings.Contains(w.Body.String(), "This slug is already taken. Pick another one.") {
			t.Errorf("body lacks the errors.slug_taken text")
		}
		if len(store.created) != 1 {
			t.Errorf("mocks persisted = %d, want still 1", len(store.created))
		}
	})

	t.Run("reserved slug is reported as taken", func(t *testing.T) {
		store := &fakeCreateStore{}
		w := postCreateForm(creatingUI(t, store, 50), base("by-slugs"))
		if w.Code != http.StatusOK || len(store.created) != 0 ||
			!strings.Contains(w.Body.String(), "This slug is already taken.") {
			t.Fatalf("status = %d, persisted = %d", w.Code, len(store.created))
		}
	})

	t.Run("malformed slug re-renders without persisting", func(t *testing.T) {
		for _, slug := range []string{"abc", "-bad-slug", "bad_slug", "has space", strings.Repeat("a", 49)} {
			store := &fakeCreateStore{}
			w := postCreateForm(creatingUI(t, store, 50), base(slug))
			if w.Code != http.StatusOK || len(store.created) != 0 {
				t.Errorf("slug %q: status = %d, persisted = %d", slug, w.Code, len(store.created))
			}
		}
	})
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

func TestHomeRendersCustomSlugField(t *testing.T) {
	u := testUI(t)
	w := httptest.NewRecorder()
	u.Home(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	for _, want := range []string{
		`name="slug"`, `maxlength="48"`, "Custom slug", "my-team-api",
		"anyone can call the mock", "Slug must be 4-48",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home page missing %q", want)
		}
	}
	if strings.Contains(body, "mock.create.slug") {
		t.Error("home page leaked a raw locale key")
	}
}
