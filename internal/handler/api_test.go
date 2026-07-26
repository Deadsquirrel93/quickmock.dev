package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	legacy := &model.Mock{Slug: "legacy1"}
	v2 := a.createView(legacy)
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
