package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// adminTokenRandBytes is the entropy size of the plain token before hex
// encoding: 32 bytes → 64 hex chars, giving 256 bits of entropy.
const adminTokenRandBytes = 32

// GenerateAdminToken returns a new one-time admin token pair: plain is the
// value shown to the user exactly once (never persisted), hash is its
// SHA-256 hex digest stored in mocks.admin_token_hash.
func GenerateAdminToken() (plain, hash string, err error) {
	buf := make([]byte, adminTokenRandBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = "qm_" + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plain))
	hash = hex.EncodeToString(sum[:])
	return plain, hash, nil
}

// VerifyAdminToken reports whether plain hashes to hash, using a
// constant-time comparison. Any invalid input (empty plain/hash) returns
// false without panicking.
func VerifyAdminToken(plain, hash string) bool {
	if plain == "" || hash == "" {
		return false
	}
	sum := sha256.Sum256([]byte(plain))
	got := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
}
