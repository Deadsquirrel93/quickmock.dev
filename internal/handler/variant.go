package handler

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// servedResponse is the resolved response for one hit of a mock after the
// flaky logic (error rate, sequence) picked a variant.
type servedResponse struct {
	Variant     string // X-Mockapi-Variant value: "default", "error", "seq-<i>/<n>"
	Status      int
	Body        string
	Headers     map[string]string // step headers laid over the mock's own; nil otherwise
	ContentType string
	Route       string
}

type responseSource struct {
	Status      int
	Body        string
	Headers     map[string]string
	ContentType string
	Variants    []model.NamedVariant
	Rules       []model.ResponseRule
}

func sourceForMock(m *model.Mock) responseSource {
	return responseSource{Status: m.ResponseStatus, Body: m.ResponseBody, Headers: m.ResponseHeaders,
		ContentType: m.ContentType, Variants: m.Variants, Rules: m.Rules}
}

func sourceForRoute(route *model.MockRoute) responseSource {
	return responseSource{Status: route.ResponseStatus, Body: route.ResponseBody, Headers: route.ResponseHeaders,
		ContentType: route.ContentType, Variants: route.Variants, Rules: route.Rules}
}

func pickConfiguredVariant(src responseSource, requested string, r *http.Request, body []byte) (servedResponse, bool) {
	byName := make(map[string]model.NamedVariant, len(src.Variants))
	for _, v := range src.Variants {
		byName[v.Name] = v
	}
	name := strings.TrimSpace(requested)
	if name == "" {
		for _, rule := range src.Rules {
			if ruleMatches(rule, r, body) {
				name = rule.Variant
				break
			}
		}
	}
	v, ok := byName[name]
	if !ok {
		return servedResponse{}, false
	}
	contentType := v.ContentType
	if contentType == "" {
		contentType = src.ContentType
	}
	return servedResponse{Variant: v.Name, Status: v.Status, Body: v.Body,
		Headers: v.Headers, ContentType: contentType}, true
}

func ruleMatches(rule model.ResponseRule, r *http.Request, body []byte) bool {
	for _, condition := range rule.Conditions {
		value, exists := conditionValue(condition, r, body)
		switch condition.Operator {
		case "exists":
			if !exists {
				return false
			}
		case "equals":
			if !exists || value != condition.Value {
				return false
			}
		case "not_equals":
			if exists && value == condition.Value {
				return false
			}
		case "contains":
			if !exists || !strings.Contains(value, condition.Value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func conditionValue(c model.MatchCondition, r *http.Request, body []byte) (string, bool) {
	switch c.Source {
	case "method":
		return r.Method, true
	case "path":
		path := strings.TrimPrefix(r.URL.Path, "/m/"+chi.URLParam(r, "slug"))
		if path == "" {
			path = "/"
		}
		return path, true
	case "query":
		values, ok := r.URL.Query()[c.Key]
		if !ok || len(values) == 0 {
			return "", false
		}
		return values[0], true
	case "header":
		values, ok := r.Header[http.CanonicalHeaderKey(c.Key)]
		if !ok || len(values) == 0 {
			return "", false
		}
		return values[0], true
	case "body":
		return jsonPathValue(body, c.Key)
	default:
		return "", false
	}
}

func jsonPathValue(body []byte, path string) (string, bool) {
	var value any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return "", false
	}
	for _, part := range strings.Split(path, ".") {
		switch current := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = current[part]
			if !ok {
				return "", false
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(current) {
				return "", false
			}
			value = current[i]
		default:
			return "", false
		}
	}
	switch v := value.(type) {
	case string:
		return v, true
	case nil:
		return "", true
	default:
		b, err := json.Marshal(v)
		return string(b), err == nil
	}
}

func requestedVariant(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Quickmock-Variant")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("__quickmock_variant"))
}

func matchRoute(routes []model.MockRoute, requestPath, method string) (*model.MockRoute, string, bool) {
	pathMatched := false
	for i := range routes {
		route := &routes[i]
		if !routePathMatches(route.Path, requestPath) {
			continue
		}
		pathMatched = true
		if route.Method == model.MethodANY || string(route.Method) == method {
			return route, route.Path, true
		}
	}
	return nil, "", pathMatched
}

func routePathMatches(pattern, actual string) bool {
	clean := func(s string) []string {
		s = strings.Trim(s, "/")
		if s == "" {
			return nil
		}
		parts := strings.Split(s, "/")
		for i := range parts {
			if decoded, err := url.PathUnescape(parts[i]); err == nil {
				parts[i] = decoded
			}
		}
		return parts
	}
	p, a := clean(pattern), clean(actual)
	if len(p) != len(a) {
		return false
	}
	for i := range p {
		if strings.HasPrefix(p[i], "{") && strings.HasSuffix(p[i], "}") {
			continue
		}
		if p[i] != a[i] {
			return false
		}
	}
	return true
}

// pickVariant decides which response a hit gets. roll is rand[0,100).
// nextPos yields the 0-based shared sequence position and is only called
// when the sequence actually serves — an error hit must not consume a
// position, and plain mocks must not touch Redis at all.
// Precedence: error roll > sequence > default.
func pickVariant(m *model.Mock, roll int, nextPos func() uint64) servedResponse {
	if m.ErrorRatePct > 0 && m.ErrorResponse != nil && roll < m.ErrorRatePct {
		return servedResponse{Variant: "error", Status: m.ErrorResponse.Status, Body: m.ErrorResponse.Body}
	}
	if n := len(m.SequenceSteps); n > 0 {
		cycle := n + 1 // the main response is step 1
		idx := int(nextPos() % uint64(cycle))
		if idx == 0 {
			return servedResponse{
				Variant: fmt.Sprintf("seq-1/%d", cycle),
				Status:  m.ResponseStatus,
				Body:    m.ResponseBody,
			}
		}
		st := m.SequenceSteps[idx-1]
		return servedResponse{
			Variant: fmt.Sprintf("seq-%d/%d", idx+1, cycle),
			Status:  st.Status,
			Body:    st.Body,
			Headers: st.Headers,
		}
	}
	return servedResponse{Variant: "default", Status: m.ResponseStatus, Body: m.ResponseBody,
		Headers: m.ResponseHeaders, ContentType: m.ContentType}
}

// effectiveDelay is the sleep for this hit: the fixed delay when maxMS is
// unset, a uniform random duration in [minMS, maxMS] otherwise.
func effectiveDelay(minMS, maxMS int) time.Duration {
	if maxMS > minMS {
		minMS += rand.IntN(maxMS - minMS + 1)
	}
	return time.Duration(minMS) * time.Millisecond
}
