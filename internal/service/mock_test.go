package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// testService returns a MockService good enough for validate(): only
// maxBody is read there.
func testService() *MockService {
	return NewMockService(nil, nil, nil, 1024, 10, time.Hour)
}

func baseInput() model.MockInput {
	return model.MockInput{Method: model.MethodGET}
}

func TestValidateErrorRate(t *testing.T) {
	s := testService()

	t.Run("out of range", func(t *testing.T) {
		for _, pct := range []int{-1, 101} {
			in := baseInput()
			in.ErrorRatePct = pct
			in.ErrorResponse = &model.ResponseStep{Status: 503}
			if err := s.validate(&in); err == nil {
				t.Fatalf("pct=%d: want error, got nil", pct)
			}
		}
	})

	t.Run("error response required when rate set", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 50
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("error response dropped when rate is 0", func(t *testing.T) {
		in := baseInput()
		in.ErrorResponse = &model.ResponseStep{Status: 503, Body: "oops"}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.ErrorResponse != nil {
			t.Fatal("ErrorResponse must be normalised away when rate is 0")
		}
	})

	t.Run("error status defaults to 500", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{Body: "boom"}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.ErrorResponse.Status != 500 {
			t.Fatalf("status = %d, want 500", in.ErrorResponse.Status)
		}
	})

	t.Run("error status out of range", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{Status: 99}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("error body too large", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{Status: 503, Body: strings.Repeat("a", 1025)}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("error headers ignored", func(t *testing.T) {
		in := baseInput()
		in.ErrorRatePct = 10
		in.ErrorResponse = &model.ResponseStep{
			Status:  503,
			Headers: map[string]string{"X-Ignored": "1"},
		}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.ErrorResponse.Headers != nil {
			t.Fatal("error-response headers must be dropped (inherited from mock)")
		}
	})
}

func TestValidateSequence(t *testing.T) {
	s := testService()

	t.Run("too many steps", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = make([]model.ResponseStep, MaxSequenceSteps+1)
		for i := range in.SequenceSteps {
			in.SequenceSteps[i] = model.ResponseStep{Status: 200}
		}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("step status defaults to 200", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{Body: "x"}}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if in.SequenceSteps[0].Status != 200 {
			t.Fatalf("status = %d, want 200", in.SequenceSteps[0].Status)
		}
	})

	t.Run("step status out of range", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{Status: 999}}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("step body too large", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{Status: 200, Body: strings.Repeat("a", 1025)}}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("step reserved headers stripped, custom kept", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{
			Status:  200,
			Headers: map[string]string{"Set-Cookie": "x=1", "X-Step": "two"},
		}}
		if err := s.validate(&in); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h := in.SequenceSteps[0].Headers
		if _, ok := h["Set-Cookie"]; ok {
			t.Fatal("reserved header must be stripped from step")
		}
		if h["X-Step"] != "two" {
			t.Fatalf("custom header lost: %v", h)
		}
	})

	t.Run("step invalid header name rejected", func(t *testing.T) {
		in := baseInput()
		in.SequenceSteps = []model.ResponseStep{{
			Status:  200,
			Headers: map[string]string{"bad name": "x"},
		}}
		if err := s.validate(&in); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}

func TestValidateDelayJitter(t *testing.T) {
	s := testService()

	cases := []struct {
		name    string
		min, mx int
		wantErr bool
	}{
		{"max unset is fine", 1000, 0, false},
		{"valid range", 100, 2000, false},
		{"max equals min", 100, 100, false},
		{"max below min", 1000, 500, true},
		{"max above cap", 0, 31000, true},
		{"negative max", 0, -5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := baseInput()
			in.ResponseDelayMS = c.min
			in.ResponseDelayMaxMS = c.mx
			err := s.validate(&in)
			if c.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
