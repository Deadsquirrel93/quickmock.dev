package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestPaymentRequestBlocked(t *testing.T) {
	tests := []struct {
		name    string
		in      model.MockInput
		blocked bool
	}{
		{
			name: "v1 body on mainnet is blocked",
			in: model.MockInput{
				ResponseBody: `{"x402Version":1,"accepts":[{"network":"base","payTo":"0xabc"}]}`,
			},
			blocked: true,
		},
		{
			name: "v1 body on base-sepolia is allowed",
			in: model.MockInput{
				ResponseBody: `{"x402Version":1,"accepts":[{"network":"base-sepolia","payTo":"0xabc"}]}`,
			},
			blocked: false,
		},
		{
			name: "v2 CAIP-2 mainnet inside a route body is blocked, neutral top-level body",
			in: model.MockInput{
				ResponseBody: `{"ok":true}`,
				Routes: []model.MockRoute{
					{ResponseBody: `{"x402Version":2,"accepts":[{"network":"eip155:8453"}]}`},
				},
			},
			blocked: true,
		},
		{
			name: "v2 CAIP-2 testnet inside a route body is allowed",
			in: model.MockInput{
				Routes: []model.MockRoute{
					{ResponseBody: `{"x402Version":2,"accepts":[{"network":"eip155:84532"}]}`},
				},
			},
			blocked: false,
		},
		{
			name: "neutral body, mainnet base64'd in PAYMENT-REQUIRED header",
			in: model.MockInput{
				ResponseBody: `{"ok":true}`,
				ResponseHeaders: map[string]string{
					"PAYMENT-REQUIRED": base64Encode(`{"x402Version":2,"accepts":[{"network":"eip155:8453"}]}`),
				},
			},
			blocked: true,
		},
		{
			name: "same header, testnet network inside is allowed",
			in: model.MockInput{
				ResponseHeaders: map[string]string{
					"PAYMENT-REQUIRED": base64Encode(`{"x402Version":2,"accepts":[{"network":"eip155:84532"}]}`),
				},
			},
			blocked: false,
		},
		{
			name: "header name lower case, mainnet, is blocked",
			in: model.MockInput{
				ResponseHeaders: map[string]string{
					"payment-required": base64Encode(`{"x402Version":2,"accepts":[{"network":"eip155:8453"}]}`),
				},
			},
			blocked: true,
		},
		{
			name: "well-known x402 route path, mainnet network, no x402Version marker",
			in: model.MockInput{
				Routes: []model.MockRoute{
					{
						Path:         "/.well-known/x402",
						ResponseBody: `{"accepts":[{"network":"eip155:1"}]}`,
					},
				},
			},
			blocked: true,
		},
		{
			name: "status-402-style body with no x402 marker is allowed",
			in: model.MockInput{
				ResponseBody: `{"error":"payment required"}`,
			},
			blocked: false,
		},
		{
			name: `"network":"base" without any x402 marker is allowed`,
			in: model.MockInput{
				ResponseBody: `{"network":"base"}`,
			},
			blocked: false,
		},
		{
			name: "x402Version present, no network anywhere, is allowed",
			in: model.MockInput{
				ResponseBody: `{"x402Version":1,"accepts":[{"payTo":"0xabc"}]}`,
			},
			blocked: false,
		},
		{
			name: "unknown network with marker is blocked (fail closed)",
			in: model.MockInput{
				ResponseBody: `{"x402Version":2,"accepts":[{"network":"eip155:999999"}]}`,
			},
			blocked: true,
		},
		{
			name: "mainnet only inside a nested route variant body is blocked",
			in: model.MockInput{
				Routes: []model.MockRoute{
					{
						ResponseBody: `{"ok":true}`,
						Variants: []model.NamedVariant{
							{Name: "insufficient-funds", Body: `{"x402Version":1,"accepts":[{"network":"base"}]}`},
						},
					},
				},
			},
			blocked: true,
		},
		{
			name: "mainnet in ErrorResponse.Body, marker in ErrorResponse.Headers name",
			in: model.MockInput{
				ErrorResponse: &model.ResponseStep{
					Body:    `{"accepts":[{"network":"base"}]}`,
					Headers: map[string]string{"PAYMENT-REQUIRED": "ignored-value"},
				},
			},
			blocked: true,
		},
		{
			name: "mixed testnet + mainnet accepts is blocked",
			in: model.MockInput{
				ResponseBody: `{"x402Version":1,"accepts":[{"network":"base-sepolia"},{"network":"base"}]}`,
			},
			blocked: true,
		},
		{
			name: "solana devnet in mixed case is allowed",
			in: model.MockInput{
				ResponseBody: `{"x402Version":1,"accepts":[{"network":"solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"}]}`,
			},
			blocked: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paymentRequestBlocked(&tt.in)
			if got != tt.blocked {
				t.Errorf("paymentRequestBlocked() = %v, want %v", got, tt.blocked)
			}
		})
	}
}

func TestPaymentRequestBlockedNilSafe(t *testing.T) {
	if paymentRequestBlocked(nil) {
		t.Error("nil input must not be blocked")
	}
}

func TestCreateRejectsPaymentRequestWithoutSpamFilter(t *testing.T) {
	s := NewMockService(nil, nil, nil, 1024, 10, time.Hour, 720*time.Hour, nil)
	_, err := s.Create(context.Background(), model.MockInput{
		Method:       model.MethodGET,
		ResponseBody: `{"x402Version":1,"accepts":[{"network":"base","payTo":"0xabc"}]}`,
	}, "198.51.100.7")
	if !errors.Is(err, ErrPaymentBlocked) {
		t.Fatalf("err = %v, want ErrPaymentBlocked", err)
	}
}
