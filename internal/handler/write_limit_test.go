package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// fakeWriteLimiter answers with a canned decision or error, so the throttled
// branch can be exercised without Redis.
type fakeWriteLimiter struct {
	allowed bool
	err     error
	keys    []string
}

func (f *fakeWriteLimiter) Allow(_ context.Context, key string) (repository.Decision, error) {
	f.keys = append(f.keys, key)
	if f.err != nil {
		return repository.Decision{}, f.err
	}
	return repository.Decision{Allowed: f.allowed}, nil
}

func TestWriteAllowed(t *testing.T) {
	tests := []struct {
		name    string
		limiter writeLimiter
		want    bool
	}{
		{name: "quota left", limiter: &fakeWriteLimiter{allowed: true}, want: true},
		{name: "quota exhausted", limiter: &fakeWriteLimiter{allowed: false}, want: false},
		{
			name:    "limiter error fails open",
			limiter: &fakeWriteLimiter{err: errors.New("redis down")},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &UI{writes: tt.limiter}
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if got := u.writeAllowed(req); got != tt.want {
				t.Fatalf("writeAllowed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteAllowedUsesItsOwnBucket(t *testing.T) {
	f := &fakeWriteLimiter{allowed: true}
	u := &UI{writes: f}
	u.writeAllowed(httptest.NewRequest(http.MethodPost, "/", nil))

	if len(f.keys) != 1 {
		t.Fatalf("limiter consulted %d times, want 1", len(f.keys))
	}
	// Sharing the "ip" or "api" bucket would let browsing mocks or calling the
	// API eat the create quota (and vice versa).
	if !strings.HasPrefix(f.keys[0], "rl:uiwrite:") {
		t.Fatalf("bucket key = %q, want the rl:uiwrite: prefix", f.keys[0])
	}
}

func TestTemplateCreateThrottledRendersBanner(t *testing.T) {
	u := testUI(t)
	u.writes = &fakeWriteLimiter{allowed: false}

	w := httptest.NewRecorder()
	r := withSlug(httptest.NewRequest(http.MethodPost, "/templates/stripe-webhook/create", nil), "stripe-webhook")
	u.TemplateCreate(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("throttled create must not redirect, got Location %q", loc)
	}
	body := w.Body.String()
	want := u.localz.T("en", "errors.rate_limit")
	if !strings.Contains(body, want) {
		t.Fatalf("page is missing the rate-limit banner %q", want)
	}
	if strings.Contains(body, "%!") {
		t.Fatal("banner rendered with a formatting artifact")
	}
}

func TestTemplateCreateUnknownSlugSkipsTheLimiter(t *testing.T) {
	f := &fakeWriteLimiter{allowed: true}
	u := testUI(t)
	u.writes = f

	w := httptest.NewRecorder()
	r := withSlug(httptest.NewRequest(http.MethodPost, "/templates/nope/create", nil), "nope")
	u.TemplateCreate(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	// A 404 costs no quota: otherwise guessing slugs would drain a visitor's
	// budget for real creates.
	if len(f.keys) != 0 {
		t.Fatalf("limiter consulted %d times on an unknown slug, want 0", len(f.keys))
	}
}
