package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPBlocklist(t *testing.T) {
	blocklist, err := NewIPBlocklist([]string{"203.0.113.7", "2001:db8:abcd::/48"})
	if err != nil {
		t.Fatalf("NewIPBlocklist: %v", err)
	}
	h := RealIP(blocklist.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	for _, tc := range []struct {
		name string
		ip   string
		want int
	}{
		{"blocked IPv4", "203.0.113.7", http.StatusForbidden},
		{"allowed IPv4", "203.0.113.8", http.StatusNoContent},
		{"blocked IPv6 prefix", "2001:db8:abcd::42", http.StatusForbidden},
		{"allowed IPv6", "2001:db8:abce::42", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = net.JoinHostPort(tc.ip, "1234")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestIPBlocklistRejectsInvalidEntry(t *testing.T) {
	if _, err := NewIPBlocklist([]string{"not-an-ip"}); err == nil {
		t.Fatal("want error, got nil")
	}
}
