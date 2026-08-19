// Package model holds the plain Go structures that flow between layers.
package model

import (
	"time"
)

// Method is the matched HTTP method for a mock. ANY matches every verb.
type Method string

const (
	MethodGET    Method = "GET"
	MethodPOST   Method = "POST"
	MethodPUT    Method = "PUT"
	MethodPATCH  Method = "PATCH"
	MethodDELETE Method = "DELETE"
	MethodANY    Method = "ANY"
)

// AllMethods is the canonical list used by validation and the UI dropdown.
var AllMethods = []Method{
	MethodGET, MethodPOST, MethodPUT, MethodPATCH, MethodDELETE, MethodANY,
}

// ValidMethod reports whether s is one of the allowed mock methods.
func ValidMethod(s string) bool {
	for _, m := range AllMethods {
		if string(m) == s {
			return true
		}
	}
	return false
}

// ResponseStep is one alternate response: a sequence step, or the error-rate
// alternate (which ignores Headers — the mock's own headers apply there).
// JSON tags define the JSONB shape stored in mocks.error_response /
// mocks.response_sequence.
type ResponseStep struct {
	Status  int               `json:"status"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

// NamedVariant is a deterministic response that can be selected explicitly
// with X-Quickmock-Variant / __quickmock_variant, or by a ResponseRule.
type NamedVariant struct {
	Name        string            `json:"name"`
	Status      int               `json:"status"`
	Body        string            `json:"body"`
	Headers     map[string]string `json:"headers,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
}

// MatchCondition is one safe, bounded request predicate. Regex is
// intentionally not supported: equals/contains/exists cover the common mock
// cases without exposing the public hot path to user-controlled regex cost.
type MatchCondition struct {
	Source   string `json:"source"` // method, path, query, header, body
	Key      string `json:"key,omitempty"`
	Operator string `json:"operator"` // equals, not_equals, contains, exists
	Value    string `json:"value,omitempty"`
}

// ResponseRule selects a named variant when all its conditions match.
// Rules are evaluated in order and the first match wins.
type ResponseRule struct {
	Name       string           `json:"name"`
	Variant    string           `json:"variant"`
	Conditions []MatchCondition `json:"conditions"`
}

// MockRoute is one method + path operation inside a multi-route mock
// workspace. Paths are relative to /m/<slug>, start with '/', and may contain
// OpenAPI-style placeholders such as /users/{id}.
type MockRoute struct {
	Name            string            `json:"name,omitempty"`
	Method          Method            `json:"method"`
	Path            string            `json:"path"`
	ResponseStatus  int               `json:"response_status"`
	ResponseBody    string            `json:"response_body,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ContentType     string            `json:"content_type,omitempty"`
	Variants        []NamedVariant    `json:"response_variants,omitempty"`
	Rules           []ResponseRule    `json:"response_rules,omitempty"`
}

// Mock mirrors one row of the `mocks` table.
type Mock struct {
	ID              string
	Slug            string
	Name            string
	Method          Method
	ResponseBody    string
	ResponseStatus  int
	ResponseHeaders map[string]string
	ResponseDelayMS int
	// ResponseDelayMaxMS, when > ResponseDelayMS, turns the fixed delay into
	// a uniform random sleep in [ResponseDelayMS, ResponseDelayMaxMS].
	ResponseDelayMaxMS int
	// ErrorRatePct (0–100) serves ErrorResponse instead of the normal
	// response for that share of requests, rolled per request.
	ErrorRatePct  int
	ErrorResponse *ResponseStep
	// SequenceSteps are EXTRA responses cycled after the main one:
	// hit 1 → main response, hit 2 → SequenceSteps[0], … then loop.
	SequenceSteps []ResponseStep
	Variants      []NamedVariant
	Rules         []ResponseRule
	Routes        []MockRoute
	ContentType   string
	// PathSuffix is the optional readable path the user picked, stored
	// without leading/trailing slashes (e.g. "users/123"). Cosmetic only:
	// any request under /m/<slug>/* still matches the mock.
	PathSuffix    string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	RequestCount  int64
	LastRequestAt *time.Time
	CreatorIP     string
	// CORSEnabled makes the serve handler emit a fixed permissive CORS preset
	// and answer OPTIONS preflight. The values are server-owned (see
	// internal/handler/mock_router.go); user-set CORS headers stay stripped.
	CORSEnabled bool
	// AdminTokenHash mirrors the `admin_token_hash` column: the SHA-256 hex
	// digest of the mock's admin token. Empty string means NULL — a legacy
	// mock created before this feature, whose mutations stay slug-only
	// until it expires.
	AdminTokenHash string
	// AdminToken is the one-time plaintext admin token. It is transient:
	// populated ONLY by MockService.Create for the single response that
	// shows it to the user, and is never read back from the database.
	AdminToken string
	// LogsPublic controls whether the live inspector is readable with the slug
	// alone. New mocks default private; owners authenticate with the admin token.
	LogsPublic  bool
	CaptureBody bool
	CaptureIP   bool
}

// MockInput is what the create/update handlers accept after parsing form or
// JSON. It's intentionally separate from Mock so the handler layer never
// touches storage-only fields like ID or CreatorIP.
type MockInput struct {
	Name               string
	Method             Method
	ResponseBody       string
	ResponseStatus     int
	ResponseHeaders    map[string]string
	ResponseDelayMS    int
	ResponseDelayMaxMS int
	ErrorRatePct       int
	ErrorResponse      *ResponseStep
	SequenceSteps      []ResponseStep
	Variants           []NamedVariant
	Rules              []ResponseRule
	Routes             []MockRoute
	ContentType        string
	PathSuffix         string
	CORSEnabled        bool
	LogsPublic         bool
	CaptureBody        bool
	CaptureIP          bool
	TTL                time.Duration
}
