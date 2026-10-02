package config

import (
	"net/netip"
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

func TestLoadParsesIPLists(t *testing.T) {
	t.Setenv("QUICKMOCK_PG_DSN", "postgres://x")
	t.Setenv("QUICKMOCK_BLOCK_IPS", " 203.0.113.7 , 2001:db8:abcd::42/48,, ")
	t.Setenv("QUICKMOCK_SPAM_ALLOW_IPS", "10.1.2.3/8")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wantBlocked := []netip.Prefix{netip.MustParsePrefix("203.0.113.7/32"), netip.MustParsePrefix("2001:db8:abcd::/48")}
	if !reflect.DeepEqual(c.BlockedIPs, wantBlocked) {
		t.Errorf("BlockedIPs = %v, want %v", c.BlockedIPs, wantBlocked)
	}
	if want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}; !reflect.DeepEqual(c.SpamAllowIPs, want) {
		t.Errorf("SpamAllowIPs = %v, want %v", c.SpamAllowIPs, want)
	}
}

func TestLoadIPListsEmptyByDefault(t *testing.T) {
	t.Setenv("QUICKMOCK_PG_DSN", "postgres://x")
	t.Setenv("QUICKMOCK_BLOCK_IPS", "")
	t.Setenv("QUICKMOCK_SPAM_ALLOW_IPS", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.BlockedIPs) != 0 || len(c.SpamAllowIPs) != 0 {
		t.Errorf("want empty lists, got %v / %v", c.BlockedIPs, c.SpamAllowIPs)
	}
}

func TestLoadRejectsInvalidIPEntry(t *testing.T) {
	t.Setenv("QUICKMOCK_PG_DSN", "postgres://x")
	for _, key := range []string{"QUICKMOCK_BLOCK_IPS", "QUICKMOCK_SPAM_ALLOW_IPS"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "203.0.113.7,not-an-ip")
			if _, err := Load(); err == nil {
				t.Fatal("want an error for an invalid entry")
			}
		})
	}
}
