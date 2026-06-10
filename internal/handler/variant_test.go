package handler

import (
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func flakyMock() *model.Mock {
	return &model.Mock{
		ResponseStatus: 200,
		ResponseBody:   "main",
		ErrorRatePct:   30,
		ErrorResponse:  &model.ResponseStep{Status: 503, Body: "err"},
		SequenceSteps: []model.ResponseStep{
			{Status: 500, Body: "step2", Headers: map[string]string{"X-Step": "2"}},
			{Status: 201, Body: "step3"},
		},
	}
}

func fixedPos(p uint64) func() uint64 { return func() uint64 { return p } }

func TestPickVariantErrorRoll(t *testing.T) {
	m := flakyMock()
	got := pickVariant(m, 29, fixedPos(0))
	if got.Variant != "error" || got.Status != 503 || got.Body != "err" {
		t.Fatalf("roll below pct must serve the error variant, got %+v", got)
	}
	if got := pickVariant(m, 30, fixedPos(0)); got.Variant == "error" {
		t.Fatalf("roll equal to pct must miss the error variant, got %+v", got)
	}
}

func TestPickVariantErrorRollSkipsCounter(t *testing.T) {
	m := flakyMock()
	called := false
	pickVariant(m, 0, func() uint64 { called = true; return 0 })
	if called {
		t.Fatal("an error hit must not consume a sequence position")
	}
}

func TestPickVariantSequenceCycles(t *testing.T) {
	m := flakyMock()
	m.ErrorRatePct = 0
	m.ErrorResponse = nil
	want := []struct {
		variant string
		status  int
		body    string
	}{
		{"seq-1/3", 200, "main"},
		{"seq-2/3", 500, "step2"},
		{"seq-3/3", 201, "step3"},
		{"seq-1/3", 200, "main"},
	}
	for pos, w := range want {
		got := pickVariant(m, 99, fixedPos(uint64(pos)))
		if got.Variant != w.variant || got.Status != w.status || got.Body != w.body {
			t.Fatalf("pos %d: got %+v, want %+v", pos, got, w)
		}
	}
}

func TestPickVariantStepHeaders(t *testing.T) {
	m := flakyMock()
	m.ErrorRatePct = 0
	got := pickVariant(m, 99, fixedPos(1))
	if got.Headers["X-Step"] != "2" {
		t.Fatalf("step headers must be carried, got %+v", got.Headers)
	}
}

func TestPickVariantPlainMock(t *testing.T) {
	m := &model.Mock{ResponseStatus: 200, ResponseBody: "main"}
	got := pickVariant(m, 0, fixedPos(0))
	if got.Variant != "default" || got.Status != 200 || got.Body != "main" {
		t.Fatalf("got %+v", got)
	}
	if got.Headers != nil {
		t.Fatalf("plain mock must not carry step headers: %+v", got.Headers)
	}
}

func TestEffectiveDelay(t *testing.T) {
	if d := effectiveDelay(100, 0); d != 100*time.Millisecond {
		t.Fatalf("fixed delay: got %v, want 100ms", d)
	}
	if d := effectiveDelay(0, 0); d != 0 {
		t.Fatalf("no delay: got %v, want 0", d)
	}
	for i := 0; i < 200; i++ {
		d := effectiveDelay(100, 200)
		if d < 100*time.Millisecond || d > 200*time.Millisecond {
			t.Fatalf("jitter out of range: %v", d)
		}
	}
}
