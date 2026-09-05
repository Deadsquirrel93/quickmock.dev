package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		header     string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "header disabled ignores spoofed XFF",
			header:     "",
			remoteAddr: "203.0.113.9:1234",
			headers:    map[string]string{"X-Forwarded-For": "9.9.9.9"},
			want:       "203.0.113.9",
		},
		{
			name:       "trusted peer, valid header value wins",
			header:     "CF-Connecting-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"CF-Connecting-IP": "203.0.113.42"},
			want:       "203.0.113.42",
		},
		{
			name:       "trusted peer, only foreign XFF sent",
			header:     "CF-Connecting-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"X-Forwarded-For": "9.9.9.9"},
			want:       "127.0.0.1",
		},
		{
			name:       "untrusted peer, header ignored",
			header:     "CF-Connecting-IP",
			remoteAddr: "203.0.113.9:1234",
			headers:    map[string]string{"CF-Connecting-IP": "203.0.113.42"},
			want:       "203.0.113.9",
		},
		{
			name:       "trusted peer, garbage header value",
			header:     "CF-Connecting-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"CF-Connecting-IP": "not-an-ip"},
			want:       "127.0.0.1",
		},
		{
			name:       "trusted peer, empty header value",
			header:     "CF-Connecting-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"CF-Connecting-IP": ""},
			want:       "127.0.0.1",
		},
		{
			name:       "trusted peer, comma-list header value rejected",
			header:     "CF-Connecting-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"CF-Connecting-IP": "1.1.1.1, 2.2.2.2"},
			want:       "127.0.0.1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotRemoteAddr, gotCtxIP string
			h := RealIP(tc.header)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotRemoteAddr = r.RemoteAddr
				gotCtxIP = IPFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if gotRemoteAddr != tc.want {
				t.Errorf("RemoteAddr = %q, want %q", gotRemoteAddr, tc.want)
			}
			if gotCtxIP != tc.want {
				t.Errorf("IPFromContext = %q, want %q", gotCtxIP, tc.want)
			}
		})
	}
}
