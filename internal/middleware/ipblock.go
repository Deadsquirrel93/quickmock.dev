package middleware

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// IPBlocklist rejects requests from configured exact addresses or CIDR
// prefixes. RealIP must run before this middleware so deployments behind a
// trusted reverse proxy match the actual client instead of the proxy hop.
type IPBlocklist struct {
	prefixes []netip.Prefix
}

func NewIPBlocklist(entries []string) (*IPBlocklist, error) {
	b := &IPBlocklist{}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("blocked IP %q: %w", entry, err)
			}
			b.prefixes = append(b.prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("blocked IP %q: %w", entry, err)
		}
		b.prefixes = append(b.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return b, nil
}

func (b *IPBlocklist) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, err := netip.ParseAddr(IPFromContext(r.Context()))
		if err == nil {
			for _, prefix := range b.prefixes {
				if prefix.Contains(addr) {
					http.Error(w, `{"error":{"code":"forbidden","message":"Forbidden"}}`, http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
