package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestSetCORSHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setCORSHeaders(w)
	h := w.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD" {
		t.Fatalf("Allow-Methods = %q", got)
	}
	if got := h.Get("Access-Control-Allow-Headers"); got != "*, Authorization" {
		t.Fatalf("Allow-Headers = %q", got)
	}
	if got := h.Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("Max-Age = %q", got)
	}
	if h.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("preset must NOT set Allow-Credentials")
	}
}

func TestCORSPreflight(t *testing.T) {
	// enabled + OPTIONS -> handled, 204, headers present
	w := httptest.NewRecorder()
	if !corsPreflight(w, &model.Mock{CORSEnabled: true}, http.MethodOptions) {
		t.Fatal("enabled OPTIONS must be handled")
	}
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("preflight must carry the CORS preset")
	}

	// enabled + GET -> not a preflight
	w2 := httptest.NewRecorder()
	if corsPreflight(w2, &model.Mock{CORSEnabled: true}, http.MethodGet) {
		t.Fatal("GET must not be treated as preflight")
	}

	// disabled + OPTIONS -> not handled
	w3 := httptest.NewRecorder()
	if corsPreflight(w3, &model.Mock{CORSEnabled: false}, http.MethodOptions) {
		t.Fatal("disabled mock must not answer preflight")
	}
}
