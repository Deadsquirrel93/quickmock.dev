package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReadFormInputFlaky(t *testing.T) {
	form := url.Values{
		"method":                {"GET"},
		"response_status":       {"200"},
		"response_delay_ms":     {"100"},
		"response_delay_max_ms": {"2000"},
		"error_rate_pct":        {"25"},
		"error_status":          {"503"},
		"error_body":            {`{"error":"boom"}`},
		"seq_status[]":          {"500", "201"},
		"seq_body[]":            {"first", "second"},
		"seq_headers[]":         {"X-Step: one\nno colon line", ""},
		"ttl":                   {"24h"},
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	in := readFormInput(r)

	if in.ResponseDelayMaxMS != 2000 {
		t.Fatalf("delay max = %d, want 2000", in.ResponseDelayMaxMS)
	}
	if in.ErrorRatePct != 25 || in.ErrorResponse == nil ||
		in.ErrorResponse.Status != 503 || in.ErrorResponse.Body != `{"error":"boom"}` {
		t.Fatalf("error config lost: pct=%d resp=%+v", in.ErrorRatePct, in.ErrorResponse)
	}
	if len(in.SequenceSteps) != 2 {
		t.Fatalf("steps = %d, want 2", len(in.SequenceSteps))
	}
	if in.SequenceSteps[0].Status != 500 || in.SequenceSteps[0].Body != "first" {
		t.Fatalf("step 1 wrong: %+v", in.SequenceSteps[0])
	}
	if in.SequenceSteps[0].Headers["X-Step"] != "one" {
		t.Fatalf("step header lost: %+v", in.SequenceSteps[0].Headers)
	}
	if in.SequenceSteps[1].Headers != nil {
		t.Fatalf("empty header textarea must give nil map, got %+v", in.SequenceSteps[1].Headers)
	}
}

func TestReadFormInputNoErrorResponseWhenRateZero(t *testing.T) {
	form := url.Values{
		"method":       {"GET"},
		"error_status": {"503"},
		"error_body":   {"boom"},
		"ttl":          {"24h"},
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	in := readFormInput(r)
	if in.ErrorResponse != nil {
		t.Fatalf("rate 0 must not build an error response, got %+v", in.ErrorResponse)
	}
}

func TestReadFormInputSkipsEmptyStepRows(t *testing.T) {
	form := url.Values{
		"method":        {"GET"},
		"seq_status[]":  {"", "500"},
		"seq_body[]":    {"", "x"},
		"seq_headers[]": {"", ""},
		"ttl":           {"24h"},
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	in := readFormInput(r)
	if len(in.SequenceSteps) != 1 || in.SequenceSteps[0].Status != 500 {
		t.Fatalf("want 1 non-empty step, got %+v", in.SequenceSteps)
	}
}

func TestParseHeaderLines(t *testing.T) {
	got := parseHeaderLines("X-A: 1\r\nX-B:two\nbroken\n: novalue\n")
	if got["X-A"] != "1" || got["X-B"] != "two" {
		t.Fatalf("parse failed: %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("junk lines must be skipped: %v", got)
	}
	if parseHeaderLines("  \n") != nil {
		t.Fatal("blank input must give nil")
	}
}
