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
	ContentType     string
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
	Name            string
	Method          Method
	ResponseBody    string
	ResponseStatus  int
	ResponseHeaders map[string]string
	ResponseDelayMS int
	ContentType     string
	PathSuffix      string
	TTL             time.Duration
}
