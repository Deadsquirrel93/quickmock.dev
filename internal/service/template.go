package service

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// tokenRe matches {{namespace.name}} with optional whitespace inside.
// Both namespace and name are lowercase ASCII to keep the surface small —
// users putting real templating syntax (e.g. Jinja, Handlebars) into a mock
// body will use other characters and won't be mangled.
var tokenRe = regexp.MustCompile(`\{\{\s*([a-z]+)\.([a-z0-9_]+)\s*\}\}`)

// SupportedTokens lists every token RenderResponseBody can substitute.
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
	"{{now.iso8601}}",
	"{{now.unix}}",
	"{{now.unix_ms}}",
	"{{now.date}}",
	"{{now.time}}",
	"{{now.rfc1123}}",
}

// RenderResponseBody substitutes {{faker.*}} and {{now.*}} tokens with
// freshly generated values. Unknown tokens are left untouched so existing
// templating syntax in the body is not corrupted.
//
// The original body is never mutated — callers persist the raw template
// and call Render only on the serving path.
func RenderResponseBody(body string) string {
	if !strings.Contains(body, "{{") {
		return body
	}
	return tokenRe.ReplaceAllStringFunc(body, func(match string) string {
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
