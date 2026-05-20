package service

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRenderResponseBody_NoTokens(t *testing.T) {
	in := `{"hello":"world"}`
	if got := RenderResponseBody(in); got != in {
		t.Fatalf("expected body untouched, got %q", got)
	}
}

func TestRenderResponseBody_UnknownTokenIsLeftAsIs(t *testing.T) {
	in := `{"v":"{{faker.unknown}}","other":"{{custom.thing}}"}`
	if got := RenderResponseBody(in); got != in {
		t.Fatalf("expected unknown tokens preserved, got %q", got)
	}
}

func TestRenderResponseBody_FakerUUID(t *testing.T) {
	out := RenderResponseBody(`{"id":"{{faker.uuid}}"}`)
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("expected valid JSON after substitution: %v (out=%q)", err, out)
	}
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(parsed.ID) {
		t.Fatalf("expected v4 UUID, got %q", parsed.ID)
	}
}

func TestRenderResponseBody_FakerNameEmail(t *testing.T) {
	out := RenderResponseBody(`{"name":"{{faker.name}}","email":"{{faker.email}}"}`)
	var parsed struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got error %v (out=%q)", err, out)
	}
	if !strings.Contains(parsed.Name, " ") {
		t.Fatalf("expected first + last name with a space, got %q", parsed.Name)
	}
	if !strings.HasSuffix(parsed.Email, "@example.com") {
		t.Fatalf("expected email under @example.com, got %q", parsed.Email)
	}
}

func TestRenderResponseBody_NowISO8601(t *testing.T) {
	out := RenderResponseBody(`{"at":"{{now.iso8601}}"}`)
	var parsed struct {
		At string `json:"at"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got error %v (out=%q)", err, out)
	}
	parsedTime, err := time.Parse(time.RFC3339, parsed.At)
	if err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q (%v)", parsed.At, err)
	}
	if time.Since(parsedTime) > time.Minute {
		t.Fatalf("expected timestamp near now, got %v", parsedTime)
	}
}

func TestRenderResponseBody_WhitespaceTolerated(t *testing.T) {
	out := RenderResponseBody(`{{ faker.uuid }}`)
	if strings.Contains(out, "{{") {
		t.Fatalf("expected substitution despite spaces, got %q", out)
	}
}

func TestRenderResponseBody_EachHitDiffers(t *testing.T) {
	a := RenderResponseBody(`{{faker.uuid}}`)
	b := RenderResponseBody(`{{faker.uuid}}`)
	if a == b {
		t.Fatalf("two UUID renders collided — extremely unlikely")
	}
}

func TestRenderResponseBody_AllSupportedTokensResolve(t *testing.T) {
	for _, tok := range SupportedTokens {
		out := RenderResponseBody(tok)
		if out == tok {
			t.Errorf("token %q was not substituted", tok)
		}
	}
}

func TestRenderResponseBody_NowFormats(t *testing.T) {
	out := RenderResponseBody(`{"u":{{now.unix}},"ms":{{now.unix_ms}},"d":"{{now.date}}","t":"{{now.time}}"}`)
	var parsed struct {
		U  int64  `json:"u"`
		MS int64  `json:"ms"`
		D  string `json:"d"`
		T  string `json:"t"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got error %v (out=%q)", err, out)
	}
	if parsed.U <= 0 || parsed.MS <= parsed.U {
		t.Fatalf("unix/unix_ms look wrong: %d / %d", parsed.U, parsed.MS)
	}
	if _, err := time.Parse("2006-01-02", parsed.D); err != nil {
		t.Fatalf("now.date not YYYY-MM-DD: %q", parsed.D)
	}
	if _, err := time.Parse("15:04:05", parsed.T); err != nil {
		t.Fatalf("now.time not HH:MM:SS: %q", parsed.T)
	}
}

func TestRenderResponseBody_BoolIsTrueOrFalse(t *testing.T) {
	for i := 0; i < 20; i++ {
		out := RenderResponseBody(`{{faker.bool}}`)
		if out != "true" && out != "false" {
			t.Fatalf("expected true/false, got %q", out)
		}
	}
}

func TestRenderResponseBody_ColorIsHex(t *testing.T) {
	out := RenderResponseBody(`{{faker.color}}`)
	re := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	if !re.MatchString(out) {
		t.Fatalf("expected #rrggbb, got %q", out)
	}
}

func TestRenderResponseBody_IPv4(t *testing.T) {
	out := RenderResponseBody(`{{faker.ipv4}}`)
	re := regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	if !re.MatchString(out) {
		t.Fatalf("expected dotted quad, got %q", out)
	}
}
