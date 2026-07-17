package handler

// UseCase is one /guide/<slug> landing page. Localized prose (title, summary,
// why) is keyed by KeyPrefix in the locale files; the code blocks below are
// language-neutral and shown verbatim. This registry is the single source of
// truth for the index, the per-case handler, the sitemap, and the JSON-LD.
type UseCase struct {
	Slug      string
	KeyPrefix string // always "guide.case." + Slug
	// CreateBody is the JSON body of POST /api/mocks, pretty-printed so it can
	// be shown verbatim inside a curl example.
	CreateBody string
	CallVerb   string // HTTP verb used to call the mock in the example
	CallHeader string // optional extra header "Name: value" for the call ("" if none)
	CallData   string // optional request body for the call ("" if none)
	// Expect is a neutral snippet describing the response / behaviour.
	Expect        string
	UsesInspector bool // case 5: show the "open the inspector" note
}

func uc(slug, createBody, verb, header, data, expect string, inspector bool) UseCase {
	return UseCase{
		Slug:          slug,
		KeyPrefix:     "guide.case." + slug,
		CreateBody:    createBody,
		CallVerb:      verb,
		CallHeader:    header,
		CallData:      data,
		Expect:        expect,
		UsesInspector: inspector,
	}
}

// UseCases is the ordered set shown on /guide. Order = display order.
var UseCases = []UseCase{
	uc("mock-rest-api",
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"users\":[{\"id\":1,\"name\":\"Ada\"}]}"
}`,
		"GET", "", "",
		`{"users":[{"id":1,"name":"Ada"}]}`, false),

	uc("test-retry-logic",
		`{
  "method": "GET",
  "response_status": 500,
  "content_type": "application/json",
  "response_body": "{\"error\":\"upstream\"}",
  "response_sequence": [
    { "status": 200, "body": "{\"ok\":true}" }
  ]
}`,
		"GET", "", "",
		"1st call  -> 500  (X-Mockapi-Variant: seq-1/2)\n2nd call  -> 200  (seq-2/2)\n...then it cycles", false),

	uc("simulate-flaky-api",
		`{
  "method": "GET",
  "response_body": "{\"ok\":true}",
  "error_rate_pct": 30,
  "error_response": { "status": 503, "body": "{\"error\":\"unavailable\"}" },
  "response_delay_ms": 100,
  "response_delay_max_ms": 900
}`,
		"GET", "", "",
		"~30% of calls -> 503, the rest -> 200\nlatency varies 100-900 ms per call", false),

	uc("simulate-slow-api",
		`{
  "method": "GET",
  "response_body": "{\"ok\":true}",
  "response_delay_ms": 3000
}`,
		"GET", "", "",
		"the response arrives after ~3 seconds", false),

	uc("mock-webhook-receiver",
		`{
  "method": "POST",
  "response_status": 200,
  "response_body": "{\"received\":true}"
}`,
		"POST", "", `{"event":"payment.succeeded"}`,
		`{"received":true}`, true),

	uc("mock-error-response",
		`{
  "method": "GET",
  "response_status": 422,
  "content_type": "application/json",
  "response_body": "{\"error\":{\"code\":\"validation_failed\",\"fields\":[\"email\"]}}"
}`,
		"GET", "", "",
		`HTTP 422 -> {"error":{"code":"validation_failed","fields":["email"]}}`, false),

	uc("fake-json-data",
		`{
  "method": "GET",
  "content_type": "application/json",
  "response_body": "{\"id\":\"{{faker.uuid}}\",\"name\":\"{{faker.name}}\",\"email\":\"{{faker.email}}\"}"
}`,
		"GET", "", "",
		"a fresh object every call, e.g.\n{\"id\":\"7c9e...\",\"name\":\"Ada Carter\",\"email\":\"ada@example.com\"}", false),

	uc("echo-request-data",
		`{
  "method": "POST",
  "content_type": "application/json",
  "response_body": "{\"you_sent\":{{request.body}},\"method\":\"{{request.method}}\",\"trace\":\"{{request.header.x-request-id}}\"}"
}`,
		"POST", "X-Request-Id: abc-123", `{"a":1}`,
		`{"you_sent":{"a":1},"method":"POST","trace":"abc-123"}`, false),

	uc("mock-api-with-cors",
		`{
  "method": "GET",
  "content_type": "application/json",
  "response_body": "{\"ok\":true}",
  "cors_enabled": true
}`,
		"GET", "Origin: https://app.example.com", "",
		"the response carries Access-Control-Allow-Origin: *\nand OPTIONS preflight answers 204 — fetch() works from any origin", false),

	uc("manage-mocks-from-any-device",
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"ok\":true}"
}`,
		"GET", "", "",
		"GET /m/<slug> -> 200 {\"ok\":true}  (works from any device, no token)\nPUT/DELETE /api/mocks/<slug> and DELETE .../logs need Authorization: Bearer <admin_token>\nno header -> 401 admin_token_required\nwrong token -> 403 admin_token_invalid", true),
}

// UseCaseBySlug returns the case for a /guide/<slug> request.
func UseCaseBySlug(slug string) (UseCase, bool) {
	for _, c := range UseCases {
		if c.Slug == slug {
			return c, true
		}
	}
	return UseCase{}, false
}
