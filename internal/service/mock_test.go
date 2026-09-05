package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// testService returns a MockService good enough for validate(): only
// maxBody is read there.
func testService() *MockService {
	return NewMockService(nil, nil, nil, 1024, 10, time.Hour, 720*time.Hour, nil)
}

func baseInput() model.MockInput {
	return model.MockInput{Method: model.MethodGET}
}

func TestValidateErrorRate(t *testing.T) {
	s := testService()

	t.Run("out of range", func(t *testing.T) {
		for _, pct := range []int{-1, 101} {
			in := baseInput()
			in.ErrorRatePct = pct
			in.ErrorResponse = &model.ResponseStep{Status: 503}
			if err := s.validate(&in); err == nil {
				t.Fatalf("pct=%d: want error, got nil", pct)
			}
		}
	})

	t.Run("error response required when rate set", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 50
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("error response dropped when rate is 0", func(t *testing.T) {
		in := baseInput()
		in.ErrorResponse = &model.ResponseStep{Status: 503, Body: "oops"}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.ErrorResponse != nil {
			t.Fatal("ErrorResponse must be normalised away when rate is 0")
		}
	})

	t.Run("error status defaults to 500", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{Body: "boom"}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.ErrorResponse.Status != 500 {
			t.Fatalf("status = %d, want 500", in.ErrorResponse.Status)
		}
	})

	t.Run("error status out of range", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{Status: 99}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("error body too large", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{Status: 503, Body: strings.Repeat("a", 1025)}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("error headers ignored", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{
			Status:  503,
			Headers: map[string]string{"X-Ignored": "1"},
		}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.ErrorResponse.Headers != nil {
			t.Fatal("error-response headers must be dropped (inherited from mock)")
		}
	})
}

func TestValidateSequence(t *testing.T) {
	s := testService()

	t.Run("too many steps", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = make([]model.ResponseStep, MaxSequenceSteps+1)
		for i := range in.SequenceSteps {
			in.SequenceSteps[i] = model.ResponseStep{Status: 200}
		}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("step status defaults to 200", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{Body: "x"}}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.SequenceSteps[0].Status != 200 {
			t.Fatalf("status = %d, want 200", in.SequenceSteps[0].Status)
		}
	})

	t.Run("step status out of range", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{Status: 999}}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("step body too large", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{Status: 200, Body: strings.Repeat("a", 1025)}}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("step reserved headers stripped, custom kept", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{
			Status:  200,
			Headers: map[string]string{"Set-Cookie": "x=1", "X-Step": "two"},
		}}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h := in.SequenceSteps[0].Headers
		if _, ok := h["Set-Cookie"]; ok {
			t.Fatal("reserved header must be stripped from step")
		}
		if h["X-Step"] != "two" {
			t.Fatalf("custom header lost: %v", h)
		}
	})

	t.Run("step invalid header name rejected", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{
			Status:  200,
			Headers: map[string]string{"bad name": "x"},
		}}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}

func TestValidateDelayJitter(t *testing.T) {
	s := testService()

	cases := []struct {
		name    string
		min, mx int
		wantErr bool
	}{
		{"max unset is fine", 1000, 0, false},
		{"valid range", 100, 2000, false},
		{"max equals min", 100, 100, false},
		{"max below min", 1000, 500, true},
		{"max above cap", 0, 31000, true},
		{"negative max", 0, -5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := baseInput()
			in.ResponseDelayMS = c.min
			in.ResponseDelayMaxMS = c.mx
			err := s.validate(&in)
			if c.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestAuthorizeAdminToken exercises the private authorize() helper that
// Update/Delete/ClearLogs all delegate to right after fetching the mock.
// It is the single place the token business rule lives, so this is the
// load-bearing test for the mutation-guard behavior.
func TestAuthorizeAdminToken(t *testing.T) {
	plain, hash, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("GenerateAdminToken: %v", err)
	}

	cases := []struct {
		name  string
		mock  *model.Mock
		token string
		want  error
	}{
		{"hashless mock, empty token rejected", &model.Mock{}, "", ErrTokenRequired},
		{"hashless mock, any token rejected", &model.Mock{}, "qm_whatever", ErrTokenInvalid},
		{"hashed mock, correct token authorized", &model.Mock{AdminTokenHash: hash}, plain, nil},
		{"hashed mock, empty token rejected", &model.Mock{AdminTokenHash: hash}, "", ErrTokenRequired},
		{"hashed mock, wrong token rejected", &model.Mock{AdminTokenHash: hash}, "qm_" + strings.Repeat("a", 64), ErrTokenInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := authorize(c.mock, c.token); !errors.Is(got, c.want) {
				t.Fatalf("authorize() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestValidateTTLCap exercises the ttl_seconds ceiling enforced in
// validate(). It is shared by Create and Update, so a single check here
// covers both callers.
func TestValidateTTLCap(t *testing.T) {
	s := NewMockService(nil, nil, nil, 1024, 10, time.Hour, 24*time.Hour, nil)

	t.Run("above cap rejected", func(t *testing.T) {
		in := baseInput()
		in.TTL = 25 * time.Hour
		err := s.validate(&in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("validate() = %v, want *ValidationError", err)
		}
		if verr.Field != "ttl_seconds" {
			t.Fatalf("Field = %q, want %q", verr.Field, "ttl_seconds")
		}
	})

	t.Run("exactly at cap accepted", func(t *testing.T) {
		in := baseInput()
		in.TTL = 24 * time.Hour
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("zero TTL accepted (default applies later)", func(t *testing.T) {
		in := baseInput()
		in.TTL = 0
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// fakeMockStore is a minimal in-memory stand-in for *repository.MockRepo,
// satisfying the mockStore interface so Extend's Get -> authorize -> Update
// flow can be exercised without a live Postgres connection.
type fakeMockStore struct {
	mocks       map[string]*model.Mock
	updateCalls int
}

func newFakeMockStore(mocks ...*model.Mock) *fakeMockStore {
	m := make(map[string]*model.Mock, len(mocks))
	for _, mk := range mocks {
		m[mk.Slug] = mk
	}
	return &fakeMockStore{mocks: m}
}

func (f *fakeMockStore) Create(_ context.Context, m *model.Mock) error {
	f.mocks[m.Slug] = m
	return nil
}

func (f *fakeMockStore) BySlug(_ context.Context, slug string) (*model.Mock, error) {
	m, ok := f.mocks[slug]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMockStore) Update(_ context.Context, m *model.Mock) error {
	f.updateCalls++
	if _, ok := f.mocks[m.Slug]; !ok {
		return repository.ErrNotFound
	}
	cp := *m
	f.mocks[m.Slug] = &cp
	return nil
}

func (f *fakeMockStore) DeleteBySlug(_ context.Context, slug string) error {
	if _, ok := f.mocks[slug]; !ok {
		return repository.ErrNotFound
	}
	delete(f.mocks, slug)
	return nil
}

func (f *fakeMockStore) CountActiveByCreatorIP(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (f *fakeMockStore) SlugExists(_ context.Context, slug string) (bool, error) {
	_, ok := f.mocks[slug]
	return ok, nil
}

// TestExtend covers the five branches of MockService.Extend: success,
// missing/wrong token, hitting the TTL cap, and the hashless mock.
func TestExtend(t *testing.T) {
	const defaultTTL = time.Hour
	const maxTTL = 24 * time.Hour

	plain, hash, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("GenerateAdminToken: %v", err)
	}

	t.Run("success shifts expires_at by defaultTTL", func(t *testing.T) {
		created := time.Now().Add(-time.Hour)
		expires := time.Now().Add(30 * time.Minute)
		store := newFakeMockStore(&model.Mock{
			Slug:           "abc123",
			CreatedAt:      created,
			ExpiresAt:      &expires,
			AdminTokenHash: hash,
		})
		s := &MockService{repo: store, defaultTTL: defaultTTL, maxTTL: maxTTL}

		before := time.Now()
		m, err := s.Extend(context.Background(), "abc123", plain)
		if err != nil {
			t.Fatalf("Extend() error = %v", err)
		}
		wantNotBefore := before.Add(defaultTTL)
		if m.ExpiresAt == nil || m.ExpiresAt.Before(wantNotBefore) {
			t.Fatalf("ExpiresAt = %v, want >= %v", m.ExpiresAt, wantNotBefore)
		}
		if store.updateCalls != 1 {
			t.Fatalf("updateCalls = %d, want 1", store.updateCalls)
		}
	})

	t.Run("no token on hashed mock", func(t *testing.T) {
		expires := time.Now().Add(time.Hour)
		store := newFakeMockStore(&model.Mock{
			Slug:           "abc123",
			CreatedAt:      time.Now(),
			ExpiresAt:      &expires,
			AdminTokenHash: hash,
		})
		s := &MockService{repo: store, defaultTTL: defaultTTL, maxTTL: maxTTL}

		if _, err := s.Extend(context.Background(), "abc123", ""); !errors.Is(err, ErrTokenRequired) {
			t.Fatalf("err = %v, want ErrTokenRequired", err)
		}
		if store.updateCalls != 0 {
			t.Fatalf("updateCalls = %d, want 0", store.updateCalls)
		}
	})

	t.Run("wrong token on hashed mock", func(t *testing.T) {
		expires := time.Now().Add(time.Hour)
		store := newFakeMockStore(&model.Mock{
			Slug:           "abc123",
			CreatedAt:      time.Now(),
			ExpiresAt:      &expires,
			AdminTokenHash: hash,
		})
		s := &MockService{repo: store, defaultTTL: defaultTTL, maxTTL: maxTTL}

		if _, err := s.Extend(context.Background(), "abc123", "qm_"+strings.Repeat("a", 64)); !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("err = %v, want ErrTokenInvalid", err)
		}
		if store.updateCalls != 0 {
			t.Fatalf("updateCalls = %d, want 0", store.updateCalls)
		}
	})

	t.Run("mock at the TTL cap", func(t *testing.T) {
		created := time.Now().Add(-maxTTL)
		expires := created.Add(maxTTL) // exactly at the cap already
		store := newFakeMockStore(&model.Mock{
			Slug:           "capped",
			CreatedAt:      created,
			ExpiresAt:      &expires,
			AdminTokenHash: hash,
		})
		s := &MockService{repo: store, defaultTTL: defaultTTL, maxTTL: maxTTL}

		_, err := s.Extend(context.Background(), "capped", plain)
		if !errors.Is(err, ErrTTLCapReached) {
			t.Fatalf("err = %v, want ErrTTLCapReached", err)
		}
		if store.updateCalls != 0 {
			t.Fatalf("updateCalls = %d, want 0 (no write at the cap)", store.updateCalls)
		}
		got, _ := store.BySlug(context.Background(), "capped")
		if !got.ExpiresAt.Equal(expires) {
			t.Fatalf("ExpiresAt changed: got %v, want %v", got.ExpiresAt, expires)
		}
	})

	t.Run("hashless mock is not extendable", func(t *testing.T) {
		expires := time.Now().Add(time.Minute)
		store := newFakeMockStore(&model.Mock{
			Slug:      "hashless",
			CreatedAt: time.Now(),
			ExpiresAt: &expires,
			// AdminTokenHash left empty: no longer a free pass.
		})
		s := &MockService{repo: store, defaultTTL: defaultTTL, maxTTL: maxTTL}

		if _, err := s.Extend(context.Background(), "hashless", ""); !errors.Is(err, ErrTokenRequired) {
			t.Fatalf("Extend() error = %v, want %v", err, ErrTokenRequired)
		}
	})
}

// TestAuthorizeSlug exercises the Get+authorize combo that read-only paths
// outside the CRUD mutations (like the log export handler) use to reach the
// same token rule as Update/Extend/Delete/ClearLogs without duplicating it.
func TestAuthorizeSlug(t *testing.T) {
	plain, hash, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("GenerateAdminToken: %v", err)
	}

	t.Run("unknown slug", func(t *testing.T) {
		s := &MockService{repo: newFakeMockStore()}
		if _, err := s.AuthorizeSlug(context.Background(), "missing", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("no token on hashed mock", func(t *testing.T) {
		store := newFakeMockStore(&model.Mock{Slug: "abc123", AdminTokenHash: hash})
		s := &MockService{repo: store}
		if _, err := s.AuthorizeSlug(context.Background(), "abc123", ""); !errors.Is(err, ErrTokenRequired) {
			t.Fatalf("err = %v, want ErrTokenRequired", err)
		}
	})

	t.Run("wrong token on hashed mock", func(t *testing.T) {
		store := newFakeMockStore(&model.Mock{Slug: "abc123", AdminTokenHash: hash})
		s := &MockService{repo: store}
		if _, err := s.AuthorizeSlug(context.Background(), "abc123", "qm_"+strings.Repeat("a", 64)); !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("err = %v, want ErrTokenInvalid", err)
		}
	})

	t.Run("correct token on hashed mock", func(t *testing.T) {
		store := newFakeMockStore(&model.Mock{Slug: "abc123", AdminTokenHash: hash})
		s := &MockService{repo: store}
		m, err := s.AuthorizeSlug(context.Background(), "abc123", plain)
		if err != nil {
			t.Fatalf("AuthorizeSlug() error = %v", err)
		}
		if m.Slug != "abc123" {
			t.Fatalf("Slug = %q, want abc123", m.Slug)
		}
	})

	t.Run("hashless mock, empty token rejected", func(t *testing.T) {
		store := newFakeMockStore(&model.Mock{Slug: "hashless"})
		s := &MockService{repo: store}
		_, err := s.AuthorizeSlug(context.Background(), "hashless", "")
		if !errors.Is(err, ErrTokenRequired) {
			t.Fatalf("AuthorizeSlug() error = %v, want %v", err, ErrTokenRequired)
		}
	})
}

func TestCORSHeadersStayReserved(t *testing.T) {
	// The cors_enabled toggle must NOT loosen the blacklist on user-supplied
	// CORS headers — those stay server-owned only.
	for _, h := range []string{
		"Access-Control-Allow-Origin",
		"access-control-allow-credentials",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	} {
		if !IsReservedResponseHeader(h) {
			t.Fatalf("%s must stay reserved for user input", h)
		}
	}
}

func TestOriginAffectingHeadersStayReserved(t *testing.T) {
	for _, h := range []string{"Service-Worker-Allowed", "service-worker-allowed"} {
		if !IsReservedResponseHeader(h) {
			t.Fatalf("%s must stay reserved for user input", h)
		}
	}
}

func TestValidateLocationScheme(t *testing.T) {
	s := testService()
	for _, value := range []string{"/relative", "https://example.com/next", "HTTP://example.com"} {
		in := baseInput()
		in.ResponseHeaders = map[string]string{"Location": value}
		if err := s.validate(&in); err != nil {
			t.Errorf("Location %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"gopher://example.com/_payload", "javascript:alert(1)", "file:///etc/passwd"} {
		in := baseInput()
		in.ResponseHeaders = map[string]string{"Location": value}
		if err := s.validate(&in); err == nil {
			t.Errorf("unsafe Location %q accepted", value)
		}
	}
}
