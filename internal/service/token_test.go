package service

import "testing"

func TestGenerateAdminToken(t *testing.T) {
	plain, hash, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plain) != 67 {
		t.Fatalf("plain length = %d, want 67", len(plain))
	}
	if plain[:3] != "qm_" {
		t.Fatalf("plain = %q, want qm_ prefix", plain)
	}
	if len(hash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hash))
	}

	plain2, hash2, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if plain == plain2 {
		t.Fatal("two calls returned the same plain token")
	}
	if hash == hash2 {
		t.Fatal("two calls returned the same hash")
	}
}

func TestVerifyAdminToken(t *testing.T) {
	plain, hash, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	other, otherHash, err := GenerateAdminToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name  string
		plain string
		hash  string
		want  bool
	}{
		{"matching pair", plain, hash, true},
		{"wrong token", other, hash, false},
		{"token from a different pair against its own hash", other, otherHash, true},
		{"empty plain", "", hash, false},
		{"empty hash", plain, "", false},
		{"both empty", "", "", false},
		{"garbage instead of hex hash", plain, "not-hex-at-all", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifyAdminToken(tt.plain, tt.hash); got != tt.want {
				t.Fatalf("VerifyAdminToken(%q, %q) = %v, want %v", tt.plain, tt.hash, got, tt.want)
			}
		})
	}
}
