package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// A mock must never be served under a JavaScript MIME type: CORP only stops
// no-cors <script src>, so a CORS-enabled mock with a JS type could still be
// loaded as a module script. As text/plain (with nosniff) the browser refuses
// to execute it, while fetch/curl clients still get the body unchanged.
func TestServeDowngradesJavaScriptMIME(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	cases := []struct {
		name        string
		contentType string
		headers     map[string]string
		want        string
	}{
		{"application/javascript", "application/javascript", nil, "text/plain; charset=utf-8"},
		{"text/javascript with params", "text/javascript; charset=utf-8", nil, "text/plain; charset=utf-8"},
		{"mixed case", "Text/JavaScript", nil, "text/plain; charset=utf-8"},
		{"legacy alias", "application/x-javascript", nil, "text/plain; charset=utf-8"},
		{"via response header", "", map[string]string{"Content-Type": "application/ecmascript"}, "text/plain; charset=utf-8"},
		{"wasm", "application/wasm", nil, "text/plain; charset=utf-8"},
		{"json untouched", "application/json", nil, "application/json"},
		{"html untouched", "text/html", nil, "text/html"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &model.Mock{
				Slug:            "js-mime",
				Method:          model.MethodGET,
				ResponseStatus:  200,
				ResponseBody:    `alert(1)`,
				ContentType:     c.contentType,
				ResponseHeaders: c.headers,
				CORSEnabled:     true,
				ExpiresAt:       &expires,
			}
			h := servingRouter(t, m, "https://quickmock.dev")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, withSlug(httptest.NewRequest(http.MethodGet, "/m/js-mime", nil), "js-mime"))

			if got := w.Header().Get("Content-Type"); got != c.want {
				t.Errorf("Content-Type = %q, want %q", got, c.want)
			}
			if w.Body.String() != "alert(1)" {
				t.Errorf("body = %q, want it unchanged", w.Body.String())
			}
		})
	}
}
