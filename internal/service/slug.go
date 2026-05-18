package service

import (
	"context"
	"crypto/rand"
	"fmt"
)

// slugAlphabet excludes visually ambiguous characters (0/O/o, I/l/1).
// 56 characters keeps a 6-char slug at ~31 bits of entropy — plenty.
const slugAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

const slugLength = 6

// SlugChecker is satisfied by *repository.MockRepo.SlugExists.
type SlugChecker interface {
	SlugExists(ctx context.Context, slug string) (bool, error)
}

// GenerateSlug returns a 6-character slug not currently in use, retrying up
// to 5 times on collision. Failing after 5 attempts is treated as a real
// problem (alphabet exhausted or DB acting up) and surfaced to the caller.
func GenerateSlug(ctx context.Context, checker SlugChecker) (string, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		s, err := randomSlug(slugLength)
		if err != nil {
			return "", err
		}
		exists, err := checker.SlugExists(ctx, s)
		if err != nil {
			return "", fmt.Errorf("check slug uniqueness: %w", err)
		}
		if !exists {
			return s, nil
		}
	}
	return "", fmt.Errorf("could not generate a unique slug after %d attempts", maxAttempts)
}

func randomSlug(n int) (string, error) {
	out := make([]byte, n)
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		out[i] = slugAlphabet[int(b)%len(slugAlphabet)]
	}
	return string(out), nil
}
