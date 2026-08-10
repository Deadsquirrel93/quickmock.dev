package handler

import (
	"encoding/json"
	"fmt"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// TemplateKind classifies how a template's response works: a canned payload
// carrying faker/now tokens (payload), a body that echoes something back
// from the caller's own request (responder), or a generic API-style fixture
// such as an OAuth/OIDC endpoint, a paginated collection, or an error
// document (api).
type TemplateKind string

// TemplateCategory groups templates into the sections shown on /templates.
type TemplateCategory string

const (
	KindPayload   TemplateKind = "payload"
	KindResponder TemplateKind = "responder"
	KindAPI       TemplateKind = "api"

	CategoryPayments TemplateCategory = "payments"
	CategoryDevtools TemplateCategory = "devtools"
	CategoryAuth     TemplateCategory = "auth"
	CategoryGeneric  TemplateCategory = "generic"
)

// MockTemplate is one curated /templates/<slug> entry: a ready-to-use mock
// configuration that reproduces the shape of a well-known third-party API
// (Stripe, GitHub, Slack, OAuth, ...). Localized prose (title, summary, why)
// is keyed by KeyPrefix in the locale files; the code blocks below are
// language-neutral and shown verbatim. This registry is the single source
// of truth for the gallery index, the per-template handler, the sitemap,
// and the JSON-LD.
type MockTemplate struct {
	Slug, KeyPrefix string
	Category        TemplateCategory
	Kind            TemplateKind
	// CreateBody is the JSON body of POST /api/mocks, pretty-printed so it
	// can be shown verbatim inside a curl example.
	CreateBody string
	CallVerb   string // HTTP verb used to call the mock in the example
	CallHeader string // optional extra header "Name: value" for the call ("" if none)
	CallData   string // optional request body for the call ("" if none)
	// Expect is a neutral snippet describing the response / behaviour.
	Expect string
	// Fields lists the JSON paths worth calling out in the response body,
	// at most 5, in the order they should be highlighted.
	Fields []string
	// RelatedGuide is the slug of the /guide/<slug> use case this template
	// pairs with for cross-linking, or "" if none applies.
	RelatedGuide string
}

func mt(slug string, category TemplateCategory, kind TemplateKind, createBody, verb, header, data, expect string, fields []string, relatedGuide string) MockTemplate {
	return MockTemplate{
		Slug:         slug,
		KeyPrefix:    "templates.case." + slug,
		Category:     category,
		Kind:         kind,
		CreateBody:   createBody,
		CallVerb:     verb,
		CallHeader:   header,
		CallData:     data,
		Expect:       expect,
		Fields:       fields,
		RelatedGuide: relatedGuide,
	}
}

// MockTemplates is the ordered set shown on /templates. Order = display order.
var MockTemplates = []MockTemplate{
	mt("stripe-webhook", CategoryPayments, KindPayload,
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"id\":\"evt_1QsErTLl2dK9Nqi7\",\"object\":\"event\",\"type\":\"payment_intent.succeeded\",\"created\":{{now.unix}},\"data\":{\"object\":{\"id\":\"pi_3QsErTLl2dK9Nqi71m2XvBnR\",\"object\":\"payment_intent\",\"amount\":4999,\"currency\":\"usd\",\"status\":\"succeeded\"}}}"
}`,
		"GET", "", "",
		"GET /m/<slug> -> 200 a Stripe-style payment_intent.succeeded event\ndata.object.amount is in minor units (cents)",
		[]string{"id", "type", "created", "data.object.amount", "data.object.status"},
		"mock-webhook-receiver"),

	mt("shopify-order-webhook", CategoryPayments, KindPayload,
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"id\":5734891234567,\"order_number\":{{seq}},\"total_price\":\"{{faker.price}}\",\"currency\":\"USD\",\"line_items\":[{\"title\":\"Wireless Mouse\",\"quantity\":2,\"price\":\"19.99\"}],\"customer\":{\"email\":\"{{faker.email}}\"}}"
}`,
		"GET", "", "",
		"GET /m/<slug> -> 200 a Shopify-style order webhook\norder_number counts up per call, total_price/customer.email are random",
		[]string{"order_number", "total_price", "currency", "line_items", "customer.email"},
		"mock-webhook-receiver"),

	mt("github-webhook-push", CategoryDevtools, KindPayload,
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"ref\":\"refs/heads/main\",\"before\":\"a10867b14bb761a232cd80139fbd4c0d33264240\",\"after\":\"b7e1a2c3d4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9\",\"repository\":{\"full_name\":\"octocat/quickmock-demo\"},\"pusher\":{\"name\":\"octocat\"},\"commits\":[{\"id\":\"b7e1a2c3d4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9\",\"message\":\"fix: handle empty payload\",\"timestamp\":\"2024-05-01T12:00:00Z\"}]}"
}`,
		"GET", "", "",
		"GET /m/<slug> -> 200 a GitHub-style push event\none commit on refs/heads/main",
		[]string{"ref", "after", "repository.full_name", "pusher.name", "commits"},
		"mock-webhook-receiver"),

	mt("slack-events-api", CategoryDevtools, KindResponder,
		`{
  "method": "POST",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"challenge\":\"{{request.body.challenge}}\"}"
}`,
		"POST", "", `{"token":"verification-token-8f3a","challenge":"3eZbrw1aBm2rZSFEGRbdazZQb7aoEXsdMY19q6IcvKf","type":"url_verification"}`,
		`POST the url_verification payload -> 200 echoes {"challenge":"..."} back
Slack requires this exact echo to verify the Events API endpoint`,
		[]string{"challenge"},
		"echo-request-data"),

	mt("telegram-bot-webhook", CategoryDevtools, KindResponder,
		`{
  "method": "POST",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"method\":\"sendMessage\",\"chat_id\":{{request.body.message.chat.id}},\"text\":\"Thanks, got it!\"}"
}`,
		"POST", "", `{"update_id":900000001,"message":{"message_id":42,"date":1717200000,"chat":{"id":987654321,"type":"private"},"text":"/start"}}`,
		`POST a Telegram Update -> 200 {"method":"sendMessage","chat_id":987654321,"text":"Thanks, got it!"}
the webhook response itself is treated as a Bot API method call`,
		[]string{"method", "chat_id", "text"},
		"echo-request-data"),

	mt("oauth2-token-response", CategoryAuth, KindAPI,
		`{
  "method": "POST",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"access_token\":\"{{faker.uuid}}\",\"token_type\":\"Bearer\",\"expires_in\":3600,\"refresh_token\":\"{{faker.uuid}}\",\"scope\":\"read write\"}"
}`,
		"POST", "", "",
		"POST /m/<slug> -> 200 an RFC 6749-shaped token response\naccess_token/refresh_token are fresh UUIDs each call",
		[]string{"access_token", "token_type", "expires_in", "refresh_token", "scope"},
		"mock-rest-api"),

	mt("openid-configuration", CategoryAuth, KindAPI,
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "path_suffix": ".well-known/openid-configuration",
  "response_body": "{\"issuer\":\"https://quickmock.dev/m/YOUR-SLUG\",\"authorization_endpoint\":\"https://quickmock.dev/m/YOUR-SLUG/authorize\",\"token_endpoint\":\"https://quickmock.dev/m/YOUR-SLUG/token\",\"jwks_uri\":\"https://quickmock.dev/m/YOUR-SLUG/.well-known/jwks.json\",\"response_types_supported\":[\"code\"],\"subject_types_supported\":[\"public\"],\"id_token_signing_alg_values_supported\":[\"RS256\"]}"
}`,
		"GET", "", "",
		"GET /m/<slug>/.well-known/openid-configuration -> 200 an OIDC discovery document\nreplace YOUR-SLUG with the mock's real slug before using it",
		[]string{"issuer", "authorization_endpoint", "token_endpoint", "jwks_uri"},
		"mock-rest-api"),

	mt("jwks-endpoint", CategoryAuth, KindAPI,
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "path_suffix": ".well-known/jwks.json",
  "response_body": "{\"keys\":[{\"kty\":\"RSA\",\"use\":\"sig\",\"alg\":\"RS256\",\"kid\":\"quickmock-demo-2024\",\"n\":\"n6cS6BgeHi-qEzBkR5cS-MPBwt1dG9y-tGiSTw8Uov3jMu9Yiz6Oua5dg6xKnEhP20owo6iHI_MMSjU6Sb6iKSyIskDb5myN9DHC6exkDsoKPn5tPOIbzXeV0SaF-b3BejNwakl5nfHVHFMkbLCvx9KP985_5k3b7hD1UTdBOFYLIgHWTIlUkV1L9o5DoiGMm2GYWxww3tL_ZFllp4i4yyPP6c4D_J7kgFukBBZZBGe3Ou1QvzcNUYoaRNRK1PRBsC24xSfN1oZYBd4xpg6Kp3xmpsGR2nzWCviSWbvHatFusy_NqCq2Io1EAeup_i0eIN86t2-G7rn4Uoul27BQcQ\",\"e\":\"AQAB\"}]}"
}`,
		"GET", "", "",
		"GET /m/<slug>/.well-known/jwks.json -> 200 one RSA JWK\nthe key is illustrative only, it does not verify any real signature",
		[]string{"keys", "keys.0.kid", "keys.0.alg", "keys.0.n"},
		"mock-rest-api"),

	mt("paginated-collection", CategoryGeneric, KindAPI,
		`{
  "method": "GET",
  "response_status": 200,
  "content_type": "application/json",
  "response_body": "{\"data\":[{\"id\":\"{{faker.uuid}}\",\"name\":\"{{faker.name}}\",\"email\":\"{{faker.email}}\"},{\"id\":\"{{faker.uuid}}\",\"name\":\"{{faker.name}}\",\"email\":\"{{faker.email}}\"},{\"id\":\"{{faker.uuid}}\",\"name\":\"{{faker.name}}\",\"email\":\"{{faker.email}}\"}],\"page\":1,\"per_page\":3,\"total\":42,\"next\":\"https://quickmock.dev/m/YOUR-SLUG?page=2\"}"
}`,
		"GET", "", "",
		"GET /m/<slug> -> 200 a paginated list of 3 fake records\npage/per_page/total/next describe the rest of the collection",
		[]string{"data", "page", "per_page", "total", "next"},
		"fake-json-data"),

	mt("problem-json-error", CategoryGeneric, KindAPI,
		`{
  "method": "GET",
  "response_status": 422,
  "content_type": "application/problem+json",
  "response_body": "{\"type\":\"https://quickmock.dev/problems/validation-error\",\"title\":\"Your request parameters didn't validate\",\"status\":422,\"detail\":\"The 'email' field must be a valid email address.\",\"instance\":\"/m/YOUR-SLUG/requests/38f0\",\"errors\":[{\"field\":\"email\",\"message\":\"must be a valid email address\"}]}"
}`,
		"GET", "", "",
		"GET /m/<slug> -> 422 application/problem+json\nan RFC 9457 problem document with a custom errors[] extension",
		[]string{"type", "title", "status", "detail", "errors"},
		"mock-error-response"),
}

// TemplateCategories orders the sections on the /templates index.
var TemplateCategories = []TemplateCategory{
	CategoryPayments, CategoryDevtools, CategoryAuth, CategoryGeneric,
}

// templateInputs is built once at startup from MockTemplates' CreateBody, so
// request handlers never re-parse JSON on the serving path.
var templateInputs = make(map[string]model.MockInput, len(MockTemplates))

func init() {
	for _, tpl := range MockTemplates {
		var req createMockRequest
		if err := json.Unmarshal([]byte(tpl.CreateBody), &req); err != nil {
			// CreateBody is a constant asset baked into the binary, same as a
			// regexp.MustCompile pattern: a parse failure here is a bug in
			// this file, not something a request can trigger.
			panic(fmt.Sprintf("template %q: CreateBody is not valid JSON: %v", tpl.Slug, err))
		}
		templateInputs[tpl.Slug] = req.toInput()
	}
}

// TemplateBySlug returns the template for a /templates/<slug> request.
func TemplateBySlug(slug string) (MockTemplate, bool) {
	for _, t := range MockTemplates {
		if t.Slug == slug {
			return t, true
		}
	}
	return MockTemplate{}, false
}

// TemplateInput returns the parsed model.MockInput for slug, built once at
// startup from the template's CreateBody.
func TemplateInput(slug string) (model.MockInput, bool) {
	in, ok := templateInputs[slug]
	return in, ok
}

// TemplatesByCategory returns the templates in category c, in MockTemplates order.
func TemplatesByCategory(c TemplateCategory) []MockTemplate {
	var out []MockTemplate
	for _, t := range MockTemplates {
		if t.Category == c {
			out = append(out, t)
		}
	}
	return out
}
