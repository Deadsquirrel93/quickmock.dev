package config

import (
	"reflect"
	"testing"
)

func TestLegacyEnvNamesStaleKeysWithTheirReplacement(t *testing.T) {
	got := LegacyEnv([]string{
		"QUICKMOCK_ADDR=:8090",
		"MOCKAPI_PG_DSN=postgres://user:secret@localhost/db",
		"PATH=/usr/bin",
		"MOCKAPI_BASE_URL=http://localhost:8090",
		"MOCKAPI_ADDR=:8090",
		"NOT_MOCKAPI_PREFIXED=1",
	})
	want := []string{
		"MOCKAPI_ADDR -> QUICKMOCK_ADDR",
		"MOCKAPI_BASE_URL -> QUICKMOCK_BASE_URL",
		"MOCKAPI_PG_DSN -> QUICKMOCK_PG_DSN",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LegacyEnv() = %q, want %q", got, want)
	}
}

// The warning goes to the log, so it must never carry the value — MOCKAPI_PG_DSN
// holds the database password.
func TestLegacyEnvNeverLeaksValues(t *testing.T) {
	for _, s := range LegacyEnv([]string{"MOCKAPI_PG_DSN=postgres://user:hunter2@localhost/db"}) {
		if s != "MOCKAPI_PG_DSN -> QUICKMOCK_PG_DSN" {
			t.Fatalf("LegacyEnv leaked more than the name: %q", s)
		}
	}
}

func TestLegacyEnvQuietOnACleanEnvironment(t *testing.T) {
	if got := LegacyEnv([]string{"QUICKMOCK_ADDR=:8080", "HOME=/root"}); len(got) != 0 {
		t.Fatalf("LegacyEnv() = %q, want none", got)
	}
}
