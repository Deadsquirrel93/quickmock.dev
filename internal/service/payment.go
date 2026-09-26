package service

import (
	"encoding/base64"
	"regexp"
	"strings"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// x402VersionRe matches the x402 protocol marker, case-insensitively,
// wherever it appears in a mock's user-controlled content.
var x402VersionRe = regexp.MustCompile(`(?i)x402version`)

// x402NetworkRe pulls a "network" field out of an x402 payment
// requirement/response JSON payload (v1 body or v2 CAIP-2 body alike).
var x402NetworkRe = regexp.MustCompile(`"network"\s*:\s*"([^"]{1,100})"`)

// x402PaymentHeaderNames are the response header names (trimmed,
// case-insensitive) that carry an x402 payment requirement or response. v2
// base64-encodes the whole JSON payload into these header values.
var x402PaymentHeaderNames = map[string]bool{
	"payment-required":   true,
	"payment-response":   true,
	"x-payment-response": true,
}

// x402TestnetExact are full network identifiers (already lowercased) that
// are testnets regardless of naming convention.
var x402TestnetExact = map[string]bool{
	"sepolia":        true,
	"solana:devnet":  true,
	"solana:testnet": true,
	"solana:etwtrabzayq6imfeykouru166vu2xqa1": true,
}

// x402TestnetSuffixes are v1-style network name suffixes that mark a
// testnet, e.g. "base-sepolia", "avalanche-fuji", "polygon-amoy".
var x402TestnetSuffixes = []string{"-sepolia", "-testnet", "-devnet", "-fuji", "-amoy"}

// x402TestnetCAIP2 are CAIP-2 (eip155:<chainID>) testnet identifiers, per
// the x402 v1→v2 network mapping.
var x402TestnetCAIP2 = map[string]bool{
	"eip155:84532":    true,
	"eip155:11155111": true,
	"eip155:43113":    true,
	"eip155:80002":    true,
	"eip155:1328":     true,
	"eip155:11124":    true,
	"eip155:17000":    true,
	"eip155:421614":   true,
	"eip155:11155420": true,
}

// isX402Testnet reports whether network (any case, any whitespace) names a
// testnet. Anything else — including a network Quickmock doesn't
// recognize — is treated as mainnet: fail closed, since an x402 reply
// naming an unknown network can still be paid on a real chain.
func isX402Testnet(network string) bool {
	n := strings.ToLower(strings.TrimSpace(network))
	if n == "" {
		return false
	}
	if x402TestnetExact[n] || x402TestnetCAIP2[n] {
		return true
	}
	for _, suffix := range x402TestnetSuffixes {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

// decodeX402Base64 tries every base64 flavor x402 client libraries emit and
// returns the first successful decode, or "" if none succeed. Decode
// failures are not an error here — the caller only cares about payloads
// that do decode to an x402 payment requirement/response.
func decodeX402Base64(s string) string {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b)
		}
	}
	return ""
}

// x402Networks extracts every "network" value out of s.
func x402Networks(s string) []string {
	if s == "" {
		return nil
	}
	matches := x402NetworkRe.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// x402HeaderMaps collects every header map a payment marker or a
// base64-encoded payment payload could hide in.
func x402HeaderMaps(in *model.MockInput) []map[string]string {
	maps := make([]map[string]string, 0, 4+len(in.SequenceSteps)+len(in.Variants)+len(in.Routes))
	maps = append(maps, in.ResponseHeaders)
	if in.ErrorResponse != nil {
		maps = append(maps, in.ErrorResponse.Headers)
	}
	for _, st := range in.SequenceSteps {
		maps = append(maps, st.Headers)
	}
	for _, v := range in.Variants {
		maps = append(maps, v.Headers)
	}
	for _, route := range in.Routes {
		maps = append(maps, route.ResponseHeaders)
		for _, v := range route.Variants {
			maps = append(maps, v.Headers)
		}
	}
	return maps
}

// x402Paths collects PathSuffix and every route Path, the two places a
// ".well-known/x402" discovery path can appear.
func x402Paths(in *model.MockInput) []string {
	paths := make([]string, 0, 1+len(in.Routes))
	paths = append(paths, in.PathSuffix)
	for _, route := range in.Routes {
		paths = append(paths, route.Path)
	}
	return paths
}

// paymentRequestBlocked reports whether in describes an x402 payment
// request that settles on a real (mainnet) network. x402 on a testnet, or
// content with no x402 marker at all, is allowed: mocking a 402 is what an
// x402 client developer needs, but Quickmock never takes part in a real
// payment. A mock is x402 when it carries the x402Version marker, a payment
// header name, or a .well-known/x402 path; it is blocked when any "network"
// it names (in plain text or a base64 payment header) is not a known
// testnet. Pure, nil-safe: no receiver, no logging, no side effects.
func paymentRequestBlocked(in *model.MockInput) bool {
	if in == nil {
		return false
	}

	surfaces := contentFields(in)
	headerMaps := x402HeaderMaps(in)

	isX402 := false
	for _, s := range surfaces {
		if s != "" && x402VersionRe.MatchString(s) {
			isX402 = true
			break
		}
	}

	var networks []string
	for _, s := range surfaces {
		networks = append(networks, x402Networks(s)...)
	}

	for _, hm := range headerMaps {
		for name, value := range hm {
			if x402PaymentHeaderNames[strings.ToLower(strings.TrimSpace(name))] {
				isX402 = true
				networks = append(networks, x402Networks(decodeX402Base64(value))...)
			}
		}
	}

	if !isX402 {
		for _, p := range x402Paths(in) {
			if strings.Contains(strings.ToLower(p), ".well-known/x402") {
				isX402 = true
				break
			}
		}
	}

	if !isX402 || len(networks) == 0 {
		return false
	}
	for _, network := range networks {
		if !isX402Testnet(network) {
			return true
		}
	}
	return false
}
