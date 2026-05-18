package service

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// ParseCurl extracts method, headers, and body from a curl command. It
// understands the flags users actually paste: -X / --request, -H / --header,
// -d / --data / --data-raw / --data-binary, and -G. It deliberately ignores
// auth, cookie jars, output files, and certificate options.
//
// The URL is detected but not retained — we only care about how the user
// would call the future mock, not where the curl was pointed.
func ParseCurl(input string) (model.MockInput, error) {
	tokens, err := tokenizeCurl(input)
	if err != nil {
		return model.MockInput{}, err
	}
	if len(tokens) == 0 || tokens[0] != "curl" {
		return model.MockInput{}, fmt.Errorf("input must start with `curl`")
	}

	in := model.MockInput{
		Method:          model.MethodGET,
		ResponseHeaders: map[string]string{},
	}
	var bodyFromFlag string
	hasBodyFlag := false

	for i := 1; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "-X" || t == "--request":
			if i+1 >= len(tokens) {
				return in, fmt.Errorf("missing value after %s", t)
			}
			i++
			m := strings.ToUpper(tokens[i])
			if !model.ValidMethod(m) {
				return in, fmt.Errorf("unknown method: %s", m)
			}
			in.Method = model.Method(m)
		case t == "-H" || t == "--header":
			if i+1 >= len(tokens) {
				return in, fmt.Errorf("missing value after %s", t)
			}
			i++
			name, value, ok := strings.Cut(tokens[i], ":")
			if !ok {
				continue
			}
			name = strings.TrimSpace(name)
			value = strings.TrimSpace(value)
			if strings.EqualFold(name, "content-type") {
				in.ContentType = value
				continue
			}
			in.ResponseHeaders[name] = value
		case t == "-d" || t == "--data" || t == "--data-raw" || t == "--data-binary":
			if i+1 >= len(tokens) {
				return in, fmt.Errorf("missing value after %s", t)
			}
			i++
			bodyFromFlag = tokens[i]
			hasBodyFlag = true
		case strings.HasPrefix(t, "--data="):
			bodyFromFlag = strings.TrimPrefix(t, "--data=")
			hasBodyFlag = true
		case strings.HasPrefix(t, "-d="):
			bodyFromFlag = strings.TrimPrefix(t, "-d=")
			hasBodyFlag = true
		case t == "-G" || t == "--get":
			in.Method = model.MethodGET
		case strings.HasPrefix(t, "-"):
			// Unknown flag — skip the value if it looks like one took an arg.
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") &&
				!looksLikeURL(tokens[i+1]) {
				i++
			}
		}
		// URL-looking tokens are ignored on purpose.
	}

	if hasBodyFlag {
		in.ResponseBody = bodyFromFlag
		// If user gave -d without -X, curl's default is POST.
		if in.Method == model.MethodGET {
			in.Method = model.MethodPOST
		}
		if in.ContentType == "" {
			in.ContentType = "application/x-www-form-urlencoded"
		}
	}

	return in, nil
}

func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// tokenizeCurl splits a curl command line into tokens, honoring single- and
// double-quoted strings and line-continuation backslashes.
func tokenizeCurl(s string) ([]string, error) {
	// Replace `\<newline>` with a space, which is how shells join lines.
	s = strings.ReplaceAll(s, "\\\n", " ")

	var (
		tokens []string
		cur    strings.Builder
		quote  byte // 0, '\'', or '"'
	)
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
				continue
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
		case unicode.IsSpace(rune(c)):
			flush()
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
		default:
			cur.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in input")
	}
	flush()
	return tokens, nil
}
