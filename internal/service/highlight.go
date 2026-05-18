package service

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"strings"
	"unicode"
)

// HighlightJSON tokenises s as JSON and returns HTML where each token is
// wrapped in a <span> with a short class name (jk/js/jn/jb/jl/jp). CSS in
// style.css colours each class.
//
// If s isn't valid JSON we don't try to be clever — we just escape the raw
// text so the caller's <pre> still renders cleanly without crashing.
//
// Done server-side so the detail page works with JavaScript disabled and
// the rendered output is search-engine / share-preview friendly.
func HighlightJSON(s string) template.HTML {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	// Cheap validity check. If it's not JSON, return escaped plaintext.
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return template.HTML(html.EscapeString(s))
	}

	var b strings.Builder
	b.Grow(len(s) + len(s)/4) // ~25% overhead for span wrappers

	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		switch {
		case isSpace(c):
			b.WriteByte(c)
			i++
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			fmt.Fprintf(&b, `<span class="jp">%c</span>`, c)
			i++
		case c == '"':
			end := findJSONStringEnd(s, i)
			tok := s[i : end+1]
			cls := "js"
			// Lookahead: if next non-space char is ':', this is a key.
			j := end + 1
			for j < n && isSpace(s[j]) {
				j++
			}
			if j < n && s[j] == ':' {
				cls = "jk"
			}
			fmt.Fprintf(&b, `<span class="%s">%s</span>`, cls, html.EscapeString(tok))
			i = end + 1
		case c == 't' && strings.HasPrefix(s[i:], "true"):
			b.WriteString(`<span class="jb">true</span>`)
			i += 4
		case c == 'f' && strings.HasPrefix(s[i:], "false"):
			b.WriteString(`<span class="jb">false</span>`)
			i += 5
		case c == 'n' && strings.HasPrefix(s[i:], "null"):
			b.WriteString(`<span class="jl">null</span>`)
			i += 4
		case c == '-' || unicode.IsDigit(rune(c)):
			end := i + 1
			for end < n {
				d := s[end]
				if !(unicode.IsDigit(rune(d)) || d == '.' || d == 'e' || d == 'E' || d == '+' || d == '-') {
					break
				}
				end++
			}
			fmt.Fprintf(&b, `<span class="jn">%s</span>`, html.EscapeString(s[i:end]))
			i = end
		default:
			// Anything else (we already validated, so this is unlikely) —
			// escape and emit.
			b.WriteString(html.EscapeString(string(c)))
			i++
		}
	}
	return template.HTML(b.String())
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// findJSONStringEnd returns the index of the closing quote for the JSON
// string starting at s[start] (which is the opening quote). It honours
// escape sequences like \". Caller has already JSON-validated s, so an
// unterminated string is not expected — we still guard against it.
func findJSONStringEnd(s string, start int) int {
	for i := start + 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++ // skip next char (it's escaped)
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return len(s) - 1
}
