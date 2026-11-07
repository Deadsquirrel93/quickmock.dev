package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
)

// The spam filter sees the stored template at create time; template tokens
// can assemble a blocked payload only when the mock is served, so the
// rendered response is checked again.
func spamServe(t *testing.T, body, creatorIP, target string, allow []netip.Prefix) *httptest.ResponseRecorder {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pats, err := service.LoadSpamPatterns("")
	if err != nil {
		t.Fatal(err)
	}
	spam, err := service.NewSpamFilter(pats, allow, logger)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	m := &model.Mock{Slug: "tpl123", Method: model.MethodGET, ResponseStatus: 200,
		ResponseBody: body, ContentType: "application/xml", CreatorIP: creatorIP, ExpiresAt: &expires}
	store := &fakeCreateStore{created: []*model.Mock{m}}
	svc := service.NewMockService(store, nil, nil, 512*1024, 50, 168*time.Hour, 720*time.Hour, spam, nil)
	h := NewMockRouter(svc, service.NewLogWriter(nil, nil, nil, 16, logger, nil), nil, nil, logger, "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withSlug(httptest.NewRequest(http.MethodGet, target, nil), m.Slug))
	return w
}

func TestServeBlocksPayloadAssembledByTemplate(t *testing.T) {
	cases := map[string]struct{ body, target string }{
		"random.pick": {`<!DOCTYPE r [<!{{random.pick:ENTITY}} x SYSTEM "https://evil.example/a">]><r>&x;</r>`, "/m/tpl123"},
		"query echo":  {`<!DOCTYPE r [<!{{request.query.k}} x SYSTEM "https://evil.example/a">]><r>&x;</r>`, "/m/tpl123?k=ENTITY"},
	}
	for name, c := range cases {
		w := spamServe(t, c.body, "203.0.113.9", c.target, nil)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "spam_blocked") {
			t.Errorf("%s: got %d %q, want 403 spam_blocked", name, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "ENTITY") {
			t.Errorf("%s: blocked payload leaked into the response: %q", name, w.Body.String())
		}
	}
}

func TestServeKeepsLegitTemplatedBody(t *testing.T) {
	w := spamServe(t, `<order id="{{request.query.id}}"/>`, "203.0.113.9", "/m/tpl123?id=42", nil)
	if w.Code != http.StatusOK || w.Body.String() != `<order id="42"/>` {
		t.Fatalf("got %d %q, want 200 <order id=\"42\"/>", w.Code, w.Body.String())
	}
}

func TestServeSkipsRenderedCheckForAllowlistedCreator(t *testing.T) {
	allow := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	w := spamServe(t, `<!{{random.pick:ENTITY}} x SYSTEM "https://evil.example/a">`, "203.0.113.9", "/m/tpl123", allow)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %q, want 200 for an allowlisted creator", w.Code, w.Body.String())
	}
}
