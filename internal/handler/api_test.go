package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

func TestCreateMockRequestToInputFlaky(t *testing.T) {
	req := createMockRequest{
		Method:             "get",
		ResponseDelayMaxMS: 2000,
		ErrorRatePct:       25,
		ErrorResponse:      &model.ResponseStep{Status: 503, Body: "boom"},
		ResponseSequence: []model.ResponseStep{
			{Status: 500, Body: "first"},
			{Status: 201, Body: "second", Headers: map[string]string{"X-Step": "2"}},
		},
	}
	in := req.toInput()
	if in.ResponseDelayMaxMS != 2000 || in.ErrorRatePct != 25 {
		t.Fatalf("scalar fields lost: %+v", in)
	}
	if in.ErrorResponse == nil || in.ErrorResponse.Status != 503 {
		t.Fatalf("error response lost: %+v", in.ErrorResponse)
	}
	if len(in.SequenceSteps) != 2 || in.SequenceSteps[1].Headers["X-Step"] != "2" {
		t.Fatalf("sequence lost: %+v", in.SequenceSteps)
	}
}

// testAPI builds an *API with a real renderer/localizer (needed by
// writeError's i18n lookup) but no service/repo — enough to exercise the
// pure request/response-shaping and error-mapping logic without a database.
func testAPI(t *testing.T) *API {
	t.Helper()
	return &API{renderer: testUI(t).renderer, baseURL: "https://example.test"}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"absent header", "", ""},
		{"well-formed", "Bearer qm_abc123", "qm_abc123"},
		{"case-insensitive scheme", "bearer qm_abc123", "qm_abc123"},
		{"extra whitespace trimmed", "Bearer   qm_abc123  ", "qm_abc123"},
		{"wrong scheme", "Basic dXNlcjpwYXNz", ""},
		{"scheme without token", "Bearer", ""},
		{"scheme with trailing space only", "Bearer ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.header != "" {
				r.Header.Set("Authorization", c.header)
			}
			if got := bearerToken(r); got != c.want {
				t.Fatalf("bearerToken(%q) = %q, want %q", c.header, got, c.want)
			}
		})
	}
}

func TestMockViewNeverLeaksAdminToken(t *testing.T) {
	a := testAPI(t)
	m := &model.Mock{
		Slug:           "abc123",
		AdminToken:     "qm_" + strings.Repeat("a", 64),
		AdminTokenHash: strings.Repeat("b", 64),
	}
	v := a.mockView(m)
	if _, ok := v["admin_token"]; ok {
		t.Fatal("mockView must never include admin_token (Get/Update reuse it)")
	}
	if _, ok := v["admin_token_hash"]; ok {
		t.Fatal("mockView must never include admin_token_hash")
	}
}

func TestCreateViewAddsAdminTokenOnlyWhenPresent(t *testing.T) {
	a := testAPI(t)

	fresh := &model.Mock{Slug: "abc123", AdminToken: "qm_" + strings.Repeat("a", 64)}
	v := a.createView(fresh)
	tok, ok := v["admin_token"]
	if !ok || tok != fresh.AdminToken {
		t.Fatalf("createView admin_token = %v, ok=%v, want %q", tok, ok, fresh.AdminToken)
	}
	if !strings.HasPrefix(tok.(string), "qm_") {
		t.Fatalf("admin_token %q missing qm_ prefix", tok)
	}

	refetched := &model.Mock{Slug: "abc123"}
	v2 := a.createView(refetched)
	if _, ok := v2["admin_token"]; ok {
		t.Fatal("createView must omit admin_token when Mock.AdminToken is empty")
	}
}

func TestWriteServiceErrorTokenMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"token required", service.ErrTokenRequired, http.StatusUnauthorized, "admin_token_required"},
		{"token invalid", service.ErrTokenInvalid, http.StatusForbidden, "admin_token_invalid"},
		{"ttl cap reached", service.ErrTTLCapReached, http.StatusConflict, "ttl_cap_reached"},
	}
	a := testAPI(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodDelete, "/api/mocks/abc123", nil)
			a.writeServiceError(w, r, c.err)
			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, c.wantStatus)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v (%s)", err, w.Body.String())
			}
			if body.Error.Code != c.wantCode {
				t.Fatalf("error code = %q, want %q", body.Error.Code, c.wantCode)
			}
		})
	}
}

func TestServiceErrorMapping(t *testing.T) {
	for _, c := range []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{service.ErrNotFound, http.StatusNotFound, "not_found"},
		{service.ErrTokenRequired, http.StatusUnauthorized, "admin_token_required"},
		{service.ErrTokenInvalid, http.StatusForbidden, "admin_token_invalid"},
		{service.ErrBodyTooLarge, http.StatusBadRequest, "body_too_large"},
		{service.ErrMockLimitReached, http.StatusTooManyRequests, "mock_limit_reached"},
		{service.ErrSpamBlocked, http.StatusUnprocessableEntity, "spam_blocked"},
		{service.ErrPaymentBlocked, http.StatusUnprocessableEntity, "payment_blocked"},
		{service.ErrTTLCapReached, http.StatusConflict, "ttl_cap_reached"},
		{service.ErrSlugTaken, http.StatusConflict, "slug_taken"},
		{&service.ValidationError{Field: "method", Message: "bad"}, http.StatusUnprocessableEntity, "validation_failed"},
		{fmt.Errorf("wrapped: %w", service.ErrNotFound), http.StatusNotFound, "not_found"},
		{errors.New("db down"), http.StatusInternalServerError, "internal"},
	} {
		status, code := serviceError(c.err)
		if status != c.wantStatus || code != c.wantCode {
			t.Errorf("serviceError(%v) = %d %q, want %d %q", c.err, status, code, c.wantStatus, c.wantCode)
		}
	}
}

func TestCreateMockRequestCarriesSlug(t *testing.T) {
	var req createMockRequest
	if err := json.Unmarshal([]byte(`{"method":"get","slug":"My-Api"}`), &req); err != nil {
		t.Fatal(err)
	}
	if got := req.toInput().Slug; got != "My-Api" {
		t.Fatalf("toInput().Slug = %q, want raw My-Api (service normalizes)", got)
	}
}

func TestAPICreateCustomSlug(t *testing.T) {
	store := &fakeCreateStore{}
	a := testAPI(t)
	a.svc = service.NewMockService(store, nil, nil, 512*1024, 50, 168*time.Hour, 720*time.Hour, nil, []string{"blocked-one"})

	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/mocks", strings.NewReader(body))
		r.RemoteAddr = "203.0.113.5"
		a.Create(w, r)
		return w
	}
	errCode := func(t *testing.T, w *httptest.ResponseRecorder) string {
		t.Helper()
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		return body.Error.Code
	}

	w := post(`{"method":"GET","slug":"My-API"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Slug != "my-api" {
		t.Fatalf("slug in response = %q (err %v), want my-api: %s", created.Slug, err, w.Body.String())
	}

	if w := post(`{"method":"GET","slug":"my-api"}`); w.Code != http.StatusConflict || errCode(t, w) != "slug_taken" {
		t.Fatalf("duplicate: status %d code %q, want 409 slug_taken", w.Code, errCode(t, w))
	}
	if w := post(`{"method":"GET","slug":"blocked-one"}`); w.Code != http.StatusConflict || errCode(t, w) != "slug_taken" {
		t.Fatalf("blocked: status %d code %q, want 409 slug_taken", w.Code, errCode(t, w))
	}
	if w := post(`{"method":"GET","slug":"by-slugs"}`); w.Code != http.StatusConflict || errCode(t, w) != "slug_taken" {
		t.Fatalf("reserved: status %d code %q, want 409 slug_taken", w.Code, errCode(t, w))
	}
	if w := post(`{"method":"GET","slug":"no_good"}`); w.Code != http.StatusUnprocessableEntity || errCode(t, w) != "validation_failed" {
		t.Fatalf("bad format: status %d code %q, want 422 validation_failed", w.Code, errCode(t, w))
	}
	if w := post(`{"method":"GET"}`); w.Code != http.StatusCreated {
		t.Fatalf("no slug: status = %d", w.Code)
	}
}
