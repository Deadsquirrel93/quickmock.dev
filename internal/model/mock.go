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
	ContentType        string
	PathSuffix         string
	TTL                time.Duration
}
