package service

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"regexp"
	"strings"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

//go:embed spam_defaults.txt
var spamDefaults string

// SpamPatternsOff is the QUICKMOCK_SPAM_PATTERNS_FILE value that disables
// the filter entirely.
const SpamPatternsOff = "off"

// SpamFilter blocks mock create/update when user-controlled content matches
// a known spam pattern (M4.4). Patterns are RE2 — linear-time matching, so
// user input can never ReDoS the service. A nil *SpamFilter is a no-op.
type SpamFilter struct {
	patterns []*regexp.Regexp
	allow    []netip.Prefix
	logger   *slog.Logger
}

// LoadSpamPatterns resolves the pattern list for a
// QUICKMOCK_SPAM_PATTERNS_FILE value: "off" → nil (disabled), "" → embedded
// defaults, anything else → that file (one regex per line, # comments).
func LoadSpamPatterns(path string) ([]string, error) {
	switch path {
	case SpamPatternsOff:
		return nil, nil
	case "":
		return parseSpamPatterns(spamDefaults), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("spam patterns file: %w", err)
	}
	return parseSpamPatterns(string(b)), nil
}

func parseSpamPatterns(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// NewSpamFilter compiles patterns and parses the allowlist (exact IPs and
// CIDR blocks). Any invalid entry is a hard error — fail fast at startup.
func NewSpamFilter(patterns, allowIPs []string, logger *slog.Logger) (*SpamFilter, error) {
	f := &SpamFilter{logger: logger}
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("spam pattern %q: %w", p, err)
		}
		f.patterns = append(f.patterns, re)
	}
	for _, s := range allowIPs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			pfx, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, fmt.Errorf("spam allow IP %q: %w", s, err)
			}
			f.allow = append(f.allow, pfx)
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("spam allow IP %q: %w", s, err)
		}
		f.allow = append(f.allow, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return f, nil
}

func (f *SpamFilter) allowed(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, p := range f.allow {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Blocked reports whether any user-controlled field of in matches a spam
// pattern. Allowlisted creator IPs always pass (the false-positive escape
// hatch from the backlog AC).
func (f *SpamFilter) Blocked(in *model.MockInput, creatorIP string) bool {
	if f == nil || len(f.patterns) == 0 {
		return false
	}
	if creatorIP != "" && f.allowed(creatorIP) {
		return false
	}
	fields := make([]string, 0, 8)
	fields = append(fields, in.Name, in.ResponseBody, in.PathSuffix)
	for _, v := range in.ResponseHeaders {
		fields = append(fields, v)
	}
	if in.ErrorResponse != nil {
		fields = append(fields, in.ErrorResponse.Body)
	}
	for _, st := range in.SequenceSteps {
		fields = append(fields, st.Body)
		for _, v := range st.Headers {
			fields = append(fields, v)
		}
	}
	for i, re := range f.patterns {
		for _, s := range fields {
			if s != "" && re.MatchString(s) {
				f.logger.Warn("spam filter blocked mock",
					slog.Int("pattern", i),
					slog.String("ip", creatorIP))
				return true
			}
		}
	}
	return false
}
