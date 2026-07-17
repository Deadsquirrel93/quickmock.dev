package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestSafeExportName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "mock"},
		{"My Stripe Webhook!", "my-stripe-webhook"},
		{"---", "mock"},
		{"Привет мир", "mock"}, // non-latin reduces to nothing → fallback
		{"a_b.c", "a-b-c"},
	}
	for _, c := range cases {
		if got := safeExportName(c.in); got != c.want {
			t.Errorf("safeExportName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToExportShapeAndSecrets(t *testing.T) {
	m := &model.Mock{
		ID:             "11111111-1111-1111-1111-111111111111",
		Slug:           "s3cr3tslug",
		Name:           "demo",
		Method:         model.MethodGET,
		ResponseBody:   `{"ok":true}`,
		ResponseStatus: 200,
		ContentType:    "application/json",
		CreatorIP:      "192.0.2.1",
		ErrorRatePct:   25,
		ErrorResponse:  &model.ResponseStep{Status: 503, Body: "boom"},
		CORSEnabled:    true,
		AdminToken:     "qm_" + strings.Repeat("a", 64),
		AdminTokenHash: strings.Repeat("b", 64),
	}
	b, err := json.Marshal(toExport(m))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "method", "response_status", "response_body", "content_type", "error_rate_pct", "error_response", "cors_enabled"} {
		if _, ok := back[key]; !ok {
			t.Errorf("export missing key %q", key)
		}
	}
	for _, leak := range []string{"s3cr3tslug", "11111111", "192.0.2.1", "slug", "creator", "admin_token", m.AdminToken, m.AdminTokenHash} {
		if strings.Contains(s, leak) {
			t.Errorf("export leaks %q: %s", leak, s)
		}
	}
	for _, absent := range []string{"response_delay_ms", "response_sequence", "path_suffix", "response_headers"} {
		if _, ok := back[absent]; ok {
			t.Errorf("export should omit empty %q", absent)
		}
	}
}
