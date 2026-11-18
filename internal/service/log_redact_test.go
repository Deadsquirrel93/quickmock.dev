package service

import (
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// submitted returns the row Submit queued for storage and the inspector.
func submitted(t *testing.T, body string) string {
	t.Helper()
	w := &LogWriter{queue: make(chan model.RequestLog, 1)}
	w.Submit(model.RequestLog{MockID: "m", RequestBody: body})
	return (<-w.queue).RequestBody
}

// A mock used as a credential collector (prod, 2026-11-18) got
// PASSWORD=…&token=… posts; the inspector must never hand passwords back.
func TestSubmitRedactsPasswords(t *testing.T) {
	cases := map[string]string{
		"verifyId=v1&PASSWORD=s3cr%2Bt&token=abc":         "verifyId=v1&PASSWORD=[redacted]&token=abc",
		"user=bob&pwd=hunter2":                            "user=bob&pwd=[redacted]",
		"pass=x&new_password=y&passwd=z":                  "pass=[redacted]&new_password=[redacted]&passwd=[redacted]",
		`{"username":"bob","password":"hu\"nter2","n":1}`: `{"username":"bob","password":"[redacted]","n":1}`,
		`{"user": {"newPassword" : "x", "Pwd":"y"}}`:      `{"user": {"newPassword" : "[redacted]", "Pwd":"[redacted]"}}`,
	}
	for in, want := range cases {
		if got := submitted(t, in); got != want {
			t.Errorf("Submit(%q) stored %q, want %q", in, got, want)
		}
	}
}

func TestSubmitKeepsNonSecretFields(t *testing.T) {
	for _, in := range []string{
		"",
		"user=bob&password=",
		`{"password":"","passenger":"Ann","bypass":"cache","compass":"N"}`,
		"passenger=Ann&compass=N",
		`{"password_policy": {"min": 8}, "has_password": true}`,
	} {
		if got := submitted(t, in); got != in {
			t.Errorf("Submit(%q) stored %q, want it unchanged", in, got)
		}
	}
}
