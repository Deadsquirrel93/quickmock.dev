package main

import "testing"

// TestParseHeaders pins the behaviour a silent skip used to hide: a header
// the CLI cannot parse has to fail the command, because the alternative is
// printing a URL and a token as if everything worked while the mock quietly
// lacks the header the caller asked for.
func TestParseHeaders(t *testing.T) {
	t.Run("name and value are split on the first colon", func(t *testing.T) {
		got, err := parseHeaders([]string{"X-Custom: 1", "Location: /m/other"})
		if err != nil {
			t.Fatalf("parseHeaders: %v", err)
		}
		if got["X-Custom"] != "1" {
			t.Errorf("X-Custom = %q, want %q", got["X-Custom"], "1")
		}
		if got["Location"] != "/m/other" {
			t.Errorf("Location = %q, want %q", got["Location"], "/m/other")
		}
	})

	t.Run("a value may itself contain colons", func(t *testing.T) {
		got, err := parseHeaders([]string{"Link: https://example.test/next"})
		if err != nil {
			t.Fatalf("parseHeaders: %v", err)
		}
		if got["Link"] != "https://example.test/next" {
			t.Errorf("Link = %q", got["Link"])
		}
	})

	t.Run("an empty value is kept", func(t *testing.T) {
		got, err := parseHeaders([]string{"X-Empty:"})
		if err != nil {
			t.Fatalf("parseHeaders: %v", err)
		}
		if v, ok := got["X-Empty"]; !ok || v != "" {
			t.Errorf("X-Empty = %q, present = %v", v, ok)
		}
	})

	t.Run("no colon is an error, not a skip", func(t *testing.T) {
		if _, err := parseHeaders([]string{"X-Custom 1"}); err == nil {
			t.Fatal("want an error for a header with no colon")
		}
	})

	t.Run("an empty name is an error", func(t *testing.T) {
		if _, err := parseHeaders([]string{"  : 1"}); err == nil {
			t.Fatal("want an error for a header with no name")
		}
	})

	t.Run("no headers yields an empty map, not nil", func(t *testing.T) {
		got, err := parseHeaders(nil)
		if err != nil {
			t.Fatalf("parseHeaders: %v", err)
		}
		if got == nil {
			t.Fatal("want a non-nil map so response_headers serialises as {}")
		}
	})
}
