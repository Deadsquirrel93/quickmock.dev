package service

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// tokenRe matches {{namespace.name}} with optional whitespace inside.
// Both namespace and name are lowercase ASCII to keep the surface small —
// users putting real templating syntax (e.g. Jinja, Handlebars) into a mock
// body will use other characters and won't be mangled.
var tokenRe = regexp.MustCompile(`\{\{\s*([a-z]+)\.([a-z0-9_]+)\s*\}\}`)

// SupportedTokens lists the fixed-form faker/now tokens, every one of which
// resolves with no context at all — TestRenderResponseBody_AllSupportedTokensResolve
// asserts exactly that, so tokens needing context ({{seq}}) or carrying an
// argument ({{random.pick:a|b|c}}) stay out of this list even though
// RenderResponseBody substitutes them too.
// Keep in sync with the UI tooltip and the README.
var SupportedTokens = []string{
	"{{faker.name}}",
	"{{faker.firstname}}",
	"{{faker.lastname}}",
	"{{faker.username}}",
	"{{faker.email}}",
	"{{faker.phone}}",
	"{{faker.url}}",
	"{{faker.ipv4}}",
	"{{faker.uuid}}",
	"{{faker.int}}",
	"{{faker.bool}}",
	"{{faker.word}}",
	"{{faker.sentence}}",
	"{{faker.color}}",
	"{{faker.company}}",
	"{{faker.city}}",
	"{{faker.price}}",
	"{{faker.lorem}}",
	"{{now.iso8601}}",
	"{{now.unix}}",
	"{{now.unix_ms}}",
	"{{now.date}}",
	"{{now.time}}",
	"{{now.rfc1123}}",
}

// RequestTokens lists the request.* echo tokens in the illustrative form the
// UI docs and README show them. query/header/body paths are user-defined, so
// these are examples, not an exhaustive set. Keep in sync with the UI
// tooltip and the README.
var RequestTokens = []string{
	"{{request.method}}",
	"{{request.path}}",
	"{{request.ip}}",
	"{{request.query.id}}",
	"{{request.header.x-request-id}}",
	"{{request.body}}",
	"{{request.body.user.name}}",
}

// requestTokenRe matches {{request.<path>}}. It is a separate pattern from
// tokenRe because header names carry uppercase letters and dashes, and JSON
// body paths carry dots — characters the strict faker/now pattern
// deliberately rejects.
var requestTokenRe = regexp.MustCompile(`\{\{\s*request\.([A-Za-z0-9_][A-Za-z0-9_.\-]*)\s*\}\}`)

// extraTokenRe matches {{random.pick:a|b|c}} and {{seq}} — a separate
// pattern from tokenRe because the pick argument carries arbitrary text
// (including the "|" separator) that the strict namespace.name grammar
// rejects. It is applied in its own pass, after the faker/now pass
// (tokenRe): a faker/now token embedded in a pick option has therefore
// already resolved to plain text by the time this pass reads the option
// list, and — same as every other pass here — its own output is never
// rescanned, so a pick result that happens to look like a token is inserted
// verbatim instead of expanding.
var extraTokenRe = regexp.MustCompile(`\{\{\s*random\.pick:([^{}]*)\s*\}\}|\{\{\s*(seq)\s*\}\}`)

// RequestData carries the parts of an incoming request that request.* tokens
// can echo back into the response body, plus one per-hit extra unrelated to
// the request itself: Seq. Header values are substituted verbatim, including
// ones the inspector redacts in storage: nothing here is persisted, and the
// value goes only to the caller who sent it.
type RequestData struct {
	Method string
	Path   string
	IP     string
	Query  url.Values
	Header http.Header
	Body   []byte

	// Seq supplies the {{seq}} token's next value. Callers wire it to the
	// shared repository.SeqCounter already used for response-sequence
	// stepping — nil leaves {{seq}} tokens untouched, the same fallback
	// every other token here uses when the data it needs isn't available.
	Seq func() uint64
}

// RenderResponseBody substitutes {{faker.*}} and {{now.*}} tokens with
// freshly generated values. Unknown tokens — including request.* ones,
// which need request context — are left untouched so existing templating
// syntax in the body is not corrupted.
//
// The original body is never mutated — callers persist the raw template
// and call Render only on the serving path.
func RenderResponseBody(body string) string {
	return RenderResponseBodyForRequest(body, nil)
}

// RenderResponseBodyForRequest is RenderResponseBody plus {{request.*}} echo
// tokens resolved against req. Request values are substituted after the
// faker/now pass and inserted verbatim — token-looking text inside a query
// param or body field is never re-expanded.
func RenderResponseBodyForRequest(body string, req *RequestData) string {
	if !strings.Contains(body, "{{") {
		return body
	}
	body = tokenRe.ReplaceAllStringFunc(body, func(match string) string {
		parts := tokenRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		switch parts[1] {
		case "faker":
			if v, ok := fakerToken(parts[2]); ok {
				return v
			}
		case "now":
			if v, ok := nowToken(parts[2]); ok {
				return v
			}
		}
		return match
	})
	body = substituteExtraTokens(body, req)
	if req == nil {
		return body
	}
	return substituteRequestTokens(body, req)
}

// substituteExtraTokens resolves {{random.pick:a|b|c}} and {{seq}}. It runs
// after the faker/now pass above, so a faker/now token embedded in a pick
// option has already resolved to plain text by the time this pass reads the
// option list. random.pick needs no request context, so this pass applies
// even when req is nil; {{seq}} does need req.Seq and is left untouched
// without it, same as any other token this package can't resolve.
func substituteExtraTokens(body string, req *RequestData) string {
	if !strings.Contains(body, "{{") {
		return body
	}
	return extraTokenRe.ReplaceAllStringFunc(body, func(match string) string {
		parts := extraTokenRe.FindStringSubmatch(match)
		if parts[2] == "seq" {
			if req == nil || req.Seq == nil {
				return match
			}
			return strconv.FormatUint(req.Seq(), 10)
		}
		list := strings.TrimSpace(parts[1])
		if list == "" {
			return match
		}
		items := strings.Split(list, "|")
		for i, it := range items {
			items[i] = strings.TrimSpace(it)
		}
		return items[randUint32()%uint32(len(items))]
	})
}

func substituteRequestTokens(body string, req *RequestData) string {
	// The JSON body is parsed lazily, at most once, and only when a
	// {{request.body.<path>}} token is actually present.
	var parsedBody any
	var bodyParsed, bodyUnparseable bool

	return requestTokenRe.ReplaceAllStringFunc(body, func(match string) string {
		path := requestTokenRe.FindStringSubmatch(match)[1]
		switch {
		case path == "method":
			return req.Method
		case path == "path":
			return req.Path
		case path == "ip":
			return req.IP
		case path == "body":
			return string(req.Body)
		case strings.HasPrefix(path, "query."):
			name := strings.TrimPrefix(path, "query.")
			if vs, ok := req.Query[name]; ok && len(vs) > 0 {
				return vs[0]
			}
		case strings.HasPrefix(path, "header."):
			if v := req.Header.Get(strings.TrimPrefix(path, "header.")); v != "" {
				return v
			}
		case strings.HasPrefix(path, "body."):
			if !bodyParsed {
				bodyParsed = true
				bodyUnparseable = json.Unmarshal(req.Body, &parsedBody) != nil
			}
			if bodyUnparseable {
				return match
			}
			if v, ok := lookupJSONPath(parsedBody, strings.Split(strings.TrimPrefix(path, "body."), ".")); ok {
				return v
			}
		}
		return match
	})
}

// lookupJSONPath walks dot-separated segments through decoded JSON: object
// keys by name, array elements by zero-based index. Strings come back bare;
// everything else (numbers, bools, null, nested objects/arrays) is
// re-marshalled so it stays valid when dropped into a JSON template.
func lookupJSONPath(node any, segs []string) (string, bool) {
	for _, seg := range segs {
		switch cur := node.(type) {
		case map[string]any:
			v, ok := cur[seg]
			if !ok {
				return "", false
			}
			node = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(cur) {
				return "", false
			}
			node = cur[idx]
		default:
			return "", false
		}
	}
	if s, ok := node.(string); ok {
		return s, true
	}
	b, err := json.Marshal(node)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func fakerToken(name string) (string, bool) {
	switch name {
	case "name":
		return pick(firstNames) + " " + pick(lastNames), true
	case "firstname":
		return pick(firstNames), true
	case "lastname":
		return pick(lastNames), true
	case "username":
		return strings.ToLower(pick(firstNames)) + strings.ToLower(pick(lastNames)) + fmt.Sprintf("%d", randUint32()%100), true
	case "email":
		return strings.ToLower(pick(firstNames)) + "." +
			strings.ToLower(pick(lastNames)) +
			fmt.Sprintf("%d", randUint32()%1000) + "@example.com", true
	case "phone":
		return fmt.Sprintf("+1%03d%03d%04d", randUint32()%800+200, randUint32()%1000, randUint32()%10000), true
	case "url":
		return "https://example.com/" + strings.ToLower(pick(words)) + "/" + strings.ToLower(pick(words)), true
	case "ipv4":
		return fmt.Sprintf("%d.%d.%d.%d",
			randUint32()%223+1, randUint32()%256, randUint32()%256, randUint32()%255+1), true
	case "uuid":
		return fakerUUID(), true
	case "int":
		return fmt.Sprintf("%d", randUint32()%1000000), true
	case "bool":
		if randUint32()%2 == 0 {
			return "true", true
		}
		return "false", true
	case "word":
		return strings.ToLower(pick(words)), true
	case "sentence":
		return fakerSentence(), true
	case "color":
		return fmt.Sprintf("#%06x", randUint32()%0x1000000), true
	case "company":
		return pick(companies), true
	case "city":
		return pick(cities), true
	case "price":
		return fmt.Sprintf("%d.%02d", randUint32()%1000, randUint32()%100), true
	case "lorem":
		return fakerLorem(), true
	}
	return "", false
}

func nowToken(name string) (string, bool) {
	t := time.Now().UTC()
	switch name {
	case "iso8601":
		return t.Format(time.RFC3339), true
	case "unix":
		return fmt.Sprintf("%d", t.Unix()), true
	case "unix_ms":
		return fmt.Sprintf("%d", t.UnixMilli()), true
	case "date":
		return t.Format("2006-01-02"), true
	case "time":
		return t.Format("15:04:05"), true
	case "rfc1123":
		return t.Format(time.RFC1123), true
	}
	return "", false
}

func fakerUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0F) | 0x40 // version 4
	b[8] = (b[8] & 0x3F) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func fakerSentence() string {
	n := 4 + int(randUint32()%5) // 4..8 words
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = strings.ToLower(pick(words))
	}
	first := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	parts[0] = first
	return strings.Join(parts, " ") + "."
}

// fakerLorem returns a short lorem-ipsum paragraph — a few faker.sentence
// results joined together, so it reads like body copy rather than one line.
func fakerLorem() string {
	n := 2 + int(randUint32()%3) // 2..4 sentences
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fakerSentence()
	}
	return strings.Join(parts, " ")
}

func pick(list []string) string {
	return list[randUint32()%uint32(len(list))]
}

func randUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}

// Pools are ASCII-only and quote-free so substitution never breaks JSON.

var firstNames = []string{
	"Alex", "Maria", "John", "Anna", "Mike", "Olivia", "Lucas", "Emma",
	"Noah", "Sophia", "Liam", "Ava", "Ethan", "Mia", "Daniel", "Lily",
	"Henry", "Zoe", "Oscar", "Nora", "Leo", "Isla", "Adam", "Chloe",
	"Hugo", "Ruby", "Owen", "Hannah", "Jack", "Grace",
}

var lastNames = []string{
	"Smith", "Johnson", "Brown", "Taylor", "Anderson", "Thomas", "Jackson",
	"White", "Harris", "Martin", "Walker", "Hall", "Allen", "Young", "King",
	"Wright", "Scott", "Green", "Baker", "Adams", "Nelson", "Carter", "Mitchell",
	"Perez", "Roberts", "Turner", "Phillips", "Campbell", "Parker", "Evans",
}

var companies = []string{
	"Acme", "Globex", "Initech", "Umbrella", "Soylent", "Hooli", "Pied Piper",
	"Wayne Enterprises", "Stark Industries", "Cyberdyne", "Tyrell", "Aperture",
	"Black Mesa", "Massive Dynamic", "Wonka Industries",
}

var cities = []string{
	"Springfield", "Riverside", "Franklin", "Greenville", "Bristol", "Clinton",
	"Fairview", "Salem", "Madison", "Georgetown", "Arlington", "Burlington",
	"Manchester", "Marion", "Oxford",
}

var words = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing",
	"elit", "sed", "eiusmod", "tempor", "incididunt", "labore", "magna",
	"aliqua", "enim", "veniam", "nostrud", "exercitation", "ullamco", "laboris",
	"nisi", "aliquip", "commodo", "consequat", "duis", "aute", "irure",
	"reprehenderit", "voluptate", "velit", "cillum", "fugiat", "nulla",
	"pariatur", "excepteur", "occaecat", "cupidatat", "proident", "sunt",
}
