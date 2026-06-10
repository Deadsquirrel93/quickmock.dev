# Flaky-API Simulation (Epic 6) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A mock can cycle through an ordered response sequence, randomly answer with an alternate error response N% of the time, and add uniform random delay jitter — all three combinable on one mock.

**Architecture:** Four new columns on `mocks` (no joins on the hot path). Variant selection is a pure function `pickVariant` in the handler package; the shared sequence position comes from a Redis `INCR` counter with an in-process fallback. Spec: `docs/superpowers/specs/2026-06-10-flaky-api-simulation-design.md`.

**Tech Stack:** Go 1.22 (`math/rand/v2`), chi, pgx/v5 (JSONB), go-redis/v9, Alpine.js form, Go html/template, JSON locales (en/ru).

**Conventions for every commit:** Conventional Commits in English, commit straight to `main`, NO `Co-Authored-By` trailer, run `git push` immediately after every commit.

**Test command:** `make test` (runs `go test ./... -race -count=1`). Tests are pure unit tests — no Postgres/Redis needed.

---

### Task 1: Migration + model fields

**Files:**
- Create: `migrations/004_flaky.sql`
- Modify: `internal/model/mock.go`

- [ ] **Step 1: Write the migration**

Create `migrations/004_flaky.sql` (style mirrors `003_stats.sql`):

```sql
-- 004_flaky.sql — flaky-API simulation (Epic 6).
--
-- Three orthogonal per-mock settings:
--   * delay jitter:      response_delay_max_ms — when > response_delay_ms the
--                         sleep is uniform random in [delay, delay_max]
--   * error rate:        error_rate_pct + error_response {status, body}
--   * response sequence: response_sequence — EXTRA steps cycled after the
--                         main response (the main response is implicitly
--                         step 1, so old mocks need no special casing)

BEGIN;

ALTER TABLE mocks
    ADD COLUMN IF NOT EXISTS response_delay_max_ms INT      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS error_rate_pct        SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS error_response        JSONB,
    ADD COLUMN IF NOT EXISTS response_sequence     JSONB;

INSERT INTO schema_migrations (version) VALUES ('004_flaky')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
```

- [ ] **Step 2: Add model types and fields**

In `internal/model/mock.go`, after the `Method` declarations and before `Mock`, add:

```go
// ResponseStep is one alternate response: a sequence step, or the error-rate
// alternate (which ignores Headers — the mock's own headers apply there).
// JSON tags define the JSONB shape stored in mocks.error_response /
// mocks.response_sequence.
type ResponseStep struct {
	Status  int               `json:"status"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}
```

In `Mock`, after `ResponseDelayMS int`, add:

```go
	// ResponseDelayMaxMS, when > ResponseDelayMS, turns the fixed delay into
	// a uniform random sleep in [ResponseDelayMS, ResponseDelayMaxMS].
	ResponseDelayMaxMS int
	// ErrorRatePct (0–100) serves ErrorResponse instead of the normal
	// response for that share of requests, rolled per request.
	ErrorRatePct  int
	ErrorResponse *ResponseStep
	// SequenceSteps are EXTRA responses cycled after the main one:
	// hit 1 → main response, hit 2 → SequenceSteps[0], … then loop.
	SequenceSteps []ResponseStep
```

In `MockInput`, after `ResponseDelayMS int`, add:

```go
	ResponseDelayMaxMS int
	ErrorRatePct       int
	ErrorResponse      *ResponseStep
	SequenceSteps      []ResponseStep
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: clean exit, no output.

- [ ] **Step 4: Commit**

```bash
git add migrations/004_flaky.sql internal/model/mock.go
git commit -m "feat(model): flaky-simulation fields — delay jitter, error rate, response sequence"
git push
```

---

### Task 2: Service validation (TDD)

**Files:**
- Create: `internal/service/mock_test.go`
- Modify: `internal/service/mock.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/mock_test.go` (note: `package service`, same as `template_test.go`, so the unexported `validate` is reachable):

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run 'TestValidate' -v`
Expected: compile error — `MaxSequenceSteps` undefined (the new validation code doesn't exist yet).

- [ ] **Step 3: Implement validation**

In `internal/service/mock.go`:

After the `PathSuffixMaxLen` const, add:

```go
// MaxSequenceSteps caps the EXTRA sequence steps per mock (the main
// response is step 1 on top of these). The create-form UI mirrors this cap.
const MaxSequenceSteps = 10
```

At the end of `validate` (after the `PathSuffix` block, before `return nil`), add:

```go
	if in.ResponseDelayMaxMS != 0 &&
		(in.ResponseDelayMaxMS < 0 || in.ResponseDelayMaxMS < in.ResponseDelayMS || in.ResponseDelayMaxMS > 30000) {
		return &ValidationError{Field: "response_delay_max_ms", Message: "delay max out of range"}
	}
	if in.ErrorRatePct < 0 || in.ErrorRatePct > 100 {
		return &ValidationError{Field: "error_rate_pct", Message: "error rate out of range"}
	}
	if in.ErrorRatePct == 0 {
		// Normalise: an alternate response without a rate is dead config.
		in.ErrorResponse = nil
	} else {
		if in.ErrorResponse == nil {
			return &ValidationError{Field: "error_response", Message: "error response required when error rate is set"}
		}
		if err := s.validateStep(in.ErrorResponse, "error_response", 500); err != nil {
			return err
		}
		// The error response inherits the mock's headers and content-type.
		in.ErrorResponse.Headers = nil
	}
	if len(in.SequenceSteps) > MaxSequenceSteps {
		return &ValidationError{Field: "response_sequence", Message: "too many sequence steps"}
	}
	for i := range in.SequenceSteps {
		st := &in.SequenceSteps[i]
		if err := s.validateStep(st, "response_sequence", 200); err != nil {
			return err
		}
		st.Headers = cleanHeaders(st.Headers)
		if len(st.Headers) == 0 {
			st.Headers = nil
		}
		for name := range st.Headers {
			if !headerNameRegexp.MatchString(name) {
				return &ValidationError{Field: "response_sequence", Message: "invalid step header name: " + name}
			}
		}
	}
```

After `validate`, add the helper:

```go
// validateStep applies the shared rules for one alternate response. A zero
// status gets the variant's default (500 for the error response, 200 for a
// sequence step).
func (s *MockService) validateStep(st *model.ResponseStep, field string, defaultStatus int) error {
	if st.Status == 0 {
		st.Status = defaultStatus
	}
	if st.Status < 100 || st.Status > 599 {
		return &ValidationError{Field: field, Message: "step status out of range"}
	}
	if len(st.Body) > s.maxBody {
		return ErrBodyTooLarge
	}
	return nil
}
```

In `Create`, in the `m := &model.Mock{...}` literal after `ResponseDelayMS: in.ResponseDelayMS,`, add:

```go
		ResponseDelayMaxMS: in.ResponseDelayMaxMS,
		ErrorRatePct:       in.ErrorRatePct,
		ErrorResponse:      in.ErrorResponse,
		SequenceSteps:      in.SequenceSteps,
```

In `Update`, after `existing.ResponseDelayMS = in.ResponseDelayMS`, add:

```go
	existing.ResponseDelayMaxMS = in.ResponseDelayMaxMS
	existing.ErrorRatePct = in.ErrorRatePct
	existing.ErrorResponse = in.ErrorResponse
	existing.SequenceSteps = in.SequenceSteps
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/ -race -count=1`
Expected: PASS (both new and existing template tests).

- [ ] **Step 5: Commit**

```bash
git add internal/service/mock.go internal/service/mock_test.go
git commit -m "feat(service): validate flaky config — error rate, sequence steps, delay jitter"
git push
```

---

### Task 3: Repository persistence

**Files:**
- Modify: `internal/repository/mock.go`

No unit test (the test suite has no Postgres); covered by the manual happy path in Task 10 — a created flaky mock must round-trip through the DB to serve.

- [ ] **Step 1: Marshal helpers in Create and Update**

In `Create`, after the existing `headers, err := json.Marshal(...)` block, add:

```go
	errResp, seq, err := marshalFlaky(m)
	if err != nil {
		return err
	}
```

Change the INSERT to include the new columns (full replacement of the query call):

```go
	return r.pool.QueryRow(ctx, `
		INSERT INTO mocks (
			slug, name, method, response_body, response_status,
			response_headers, response_delay_ms, content_type,
			path_suffix, expires_at, creator_ip,
			response_delay_max_ms, error_rate_pct, error_response, response_sequence
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, created_at
	`,
		m.Slug, m.Name, string(m.Method), m.ResponseBody, m.ResponseStatus,
		headers, m.ResponseDelayMS, m.ContentType,
		suffix, m.ExpiresAt, m.CreatorIP,
		m.ResponseDelayMaxMS, m.ErrorRatePct, errResp, seq,
	).Scan(&m.ID, &m.CreatedAt)
```

At the bottom of the file add the helper (nil `[]byte` ⇒ SQL NULL with pgx):

```go
// marshalFlaky serialises the optional flaky-config blobs. nil slices map
// to SQL NULL so plain mocks keep NULL columns.
func marshalFlaky(m *model.Mock) (errResp, seq []byte, err error) {
	if m.ErrorResponse != nil {
		if errResp, err = json.Marshal(m.ErrorResponse); err != nil {
			return nil, nil, fmt.Errorf("marshal error response: %w", err)
		}
	}
	if len(m.SequenceSteps) > 0 {
		if seq, err = json.Marshal(m.SequenceSteps); err != nil {
			return nil, nil, fmt.Errorf("marshal response sequence: %w", err)
		}
	}
	return errResp, seq, nil
}
```

- [ ] **Step 2: Update statement**

In `Update`, after the `headers, err := json.Marshal(...)` block, add the same:

```go
	errResp, seq, err := marshalFlaky(m)
	if err != nil {
		return err
	}
```

Replace the UPDATE statement with:

```go
	tag, err := r.pool.Exec(ctx, `
		UPDATE mocks SET
			name              = $2,
			method            = $3,
			response_body     = $4,
			response_status   = $5,
			response_headers  = $6,
			response_delay_ms = $7,
			content_type      = $8,
			path_suffix       = $9,
			expires_at        = $10,
			response_delay_max_ms = $11,
			error_rate_pct        = $12,
			error_response        = $13,
			response_sequence     = $14
		WHERE slug = $1
		  AND (expires_at IS NULL OR expires_at > now())
	`,
		m.Slug, m.Name, string(m.Method), m.ResponseBody, m.ResponseStatus,
		headers, m.ResponseDelayMS, m.ContentType, suffix, m.ExpiresAt,
		m.ResponseDelayMaxMS, m.ErrorRatePct, errResp, seq,
	)
```

- [ ] **Step 3: Read path**

In `BySlug`, replace the SELECT with (new columns appended last):

```go
	row := r.pool.QueryRow(ctx, `
		SELECT id, slug, name, method, response_body, response_status,
		       response_headers, response_delay_ms, content_type,
		       path_suffix, expires_at, created_at, request_count,
		       last_request_at, creator_ip,
		       response_delay_max_ms, error_rate_pct, error_response, response_sequence
		FROM mocks
		WHERE slug = $1
		  AND (expires_at IS NULL OR expires_at > now())
	`, slug)
```

In `scanMock`, extend the locals and Scan call:

```go
	var (
		m       model.Mock
		method  string
		headers []byte
		suffix  *string
		errResp []byte
		seq     []byte
	)
	err := row.Scan(
		&m.ID, &m.Slug, &m.Name, &method, &m.ResponseBody, &m.ResponseStatus,
		&headers, &m.ResponseDelayMS, &m.ContentType, &suffix,
		&m.ExpiresAt, &m.CreatedAt, &m.RequestCount, &m.LastRequestAt, &m.CreatorIP,
		&m.ResponseDelayMaxMS, &m.ErrorRatePct, &errResp, &seq,
	)
```

And after the existing `ResponseHeaders` unmarshal block, add:

```go
	if len(errResp) > 0 {
		_ = json.Unmarshal(errResp, &m.ErrorResponse)
	}
	if len(seq) > 0 {
		_ = json.Unmarshal(seq, &m.SequenceSteps)
	}
```

- [ ] **Step 4: Verify build and existing tests**

Run: `go build ./... && go test ./... -race -count=1`
Expected: build clean, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/mock.go
git commit -m "feat(repository): persist flaky config in mocks table"
git push
```

---

### Task 4: Sequence counter (TDD)

**Files:**
- Create: `internal/repository/seq.go`
- Test: `internal/repository/seq_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/repository/seq_test.go`:

```go
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestSeqCounterMemory(t *testing.T) {
	c := NewSeqCounter(nil) // nil client ⇒ memory-only
	ctx := context.Background()

	for want := uint64(0); want < 5; want++ {
		if got := c.Next(ctx, "mock-a"); got != want {
			t.Fatalf("Next(mock-a) = %d, want %d", got, want)
		}
	}
	if got := c.Next(ctx, "mock-b"); got != 0 {
		t.Fatalf("counters must be independent per mock: got %d, want 0", got)
	}
}

func TestSeqCounterFallsBackWhenRedisDown(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listens here
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer rdb.Close()

	c := NewSeqCounter(rdb)
	ctx := context.Background()
	if got := c.Next(ctx, "mock-a"); got != 0 {
		t.Fatalf("first fallback Next = %d, want 0", got)
	}
	if got := c.Next(ctx, "mock-a"); got != 1 {
		t.Fatalf("second fallback Next = %d, want 1", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/repository/ -run TestSeqCounter -v`
Expected: compile error — `NewSeqCounter` undefined.

- [ ] **Step 3: Implement SeqCounter**

Create `internal/repository/seq.go`:

```go
package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// SeqCounter hands out per-mock, monotonically increasing hit positions for
// response sequences. Redis INCR keeps the position shared across callers
// and instances; the position is "cheap to lose" state (a Redis restart
// resets every sequence to step 1, which is acceptable — see
// ARCHITECTURE.md on what Redis may hold).
//
// When Redis is unavailable the counter falls back to an in-process atomic,
// so cycling keeps working within this instance instead of pinning every
// caller to step 1.
type SeqCounter struct {
	rdb *redis.Client // nil ⇒ memory-only (tests)
	ttl time.Duration
	mem sync.Map // mock ID → *atomic.Uint64
}

// NewSeqCounter wraps rdb. The key TTL slides on every hit; a week outlives
// any realistic test session while still cleaning up after expired mocks.
func NewSeqCounter(rdb *redis.Client) *SeqCounter {
	return &SeqCounter{rdb: rdb, ttl: 7 * 24 * time.Hour}
}

// Next returns the 0-based position for this hit of mockID.
func (c *SeqCounter) Next(ctx context.Context, mockID string) uint64 {
	if c.rdb != nil {
		key := "seq:" + mockID
		pipe := c.rdb.TxPipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, c.ttl)
		if _, err := pipe.Exec(ctx); err == nil {
			return uint64(incr.Val()) - 1
		}
	}
	v, _ := c.mem.LoadOrStore(mockID, &atomic.Uint64{})
	return v.(*atomic.Uint64).Add(1) - 1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repository/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/seq.go internal/repository/seq_test.go
git commit -m "feat(repository): Redis-backed sequence counter with in-process fallback"
git push
```

---

### Task 5: Variant selection + jitter (TDD)

**Files:**
- Create: `internal/handler/variant.go`
- Test: `internal/handler/variant_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/handler/variant_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/handler/ -run 'TestPickVariant|TestEffectiveDelay' -v`
Expected: compile error — `pickVariant` undefined.

- [ ] **Step 3: Implement**

Create `internal/handler/variant.go`:

```go
package handler

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// servedResponse is the resolved response for one hit of a mock after the
// flaky logic (error rate, sequence) picked a variant.
type servedResponse struct {
	Variant string // X-Mockapi-Variant value: "default", "error", "seq-<i>/<n>"
	Status  int
	Body    string
	Headers map[string]string // step headers laid over the mock's own; nil otherwise
}

// pickVariant decides which response a hit gets. roll is rand[0,100).
// nextPos yields the 0-based shared sequence position and is only called
// when the sequence actually serves — an error hit must not consume a
// position, and plain mocks must not touch Redis at all.
// Precedence: error roll > sequence > default.
func pickVariant(m *model.Mock, roll int, nextPos func() uint64) servedResponse {
	if m.ErrorRatePct > 0 && m.ErrorResponse != nil && roll < m.ErrorRatePct {
		return servedResponse{Variant: "error", Status: m.ErrorResponse.Status, Body: m.ErrorResponse.Body}
	}
	if n := len(m.SequenceSteps); n > 0 {
		cycle := n + 1 // the main response is step 1
		idx := int(nextPos() % uint64(cycle))
		if idx == 0 {
			return servedResponse{
				Variant: fmt.Sprintf("seq-1/%d", cycle),
				Status:  m.ResponseStatus,
				Body:    m.ResponseBody,
			}
		}
		st := m.SequenceSteps[idx-1]
		return servedResponse{
			Variant: fmt.Sprintf("seq-%d/%d", idx+1, cycle),
			Status:  st.Status,
			Body:    st.Body,
			Headers: st.Headers,
		}
	}
	return servedResponse{Variant: "default", Status: m.ResponseStatus, Body: m.ResponseBody}
}

// effectiveDelay is the sleep for this hit: the fixed delay when maxMS is
// unset, a uniform random duration in [minMS, maxMS] otherwise.
func effectiveDelay(minMS, maxMS int) time.Duration {
	if maxMS > minMS {
		minMS += rand.IntN(maxMS - minMS + 1)
	}
	return time.Duration(minMS) * time.Millisecond
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/variant.go internal/handler/variant_test.go
git commit -m "feat(handler): pure variant selection and delay jitter for flaky mocks"
git push
```

---

### Task 6: Wire the mock router + main.go

**Files:**
- Modify: `internal/handler/mock_router.go`
- Modify: `cmd/server/main.go:171`

- [ ] **Step 1: Router takes the counter and serves variants**

In `internal/handler/mock_router.go`:

Add imports `"math/rand/v2"` and `"github.com/Deadsquirrel93/quickmock.dev/internal/repository"` (the handler package already imports repository elsewhere — no cycle).

Replace the struct + constructor:

```go
type MockRouter struct {
	svc    *service.MockService
	logs   *service.LogWriter
	seq    *repository.SeqCounter
	maxLog int
}

func NewMockRouter(svc *service.MockService, logs *service.LogWriter, seq *repository.SeqCounter) *MockRouter {
	return &MockRouter{svc: svc, logs: logs, seq: seq, maxLog: 16 * 1024}
}
```

In `ServeHTTP`, replace the delay block:

```go
	if m.ResponseDelayMS > 0 {
		select {
		case <-time.After(time.Duration(m.ResponseDelayMS) * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
```

with variant selection + jitter-aware sleep:

```go
	served := pickVariant(m, rand.IntN(100), func() uint64 {
		return h.seq.Next(r.Context(), m.ID)
	})

	if d := effectiveDelay(m.ResponseDelayMS, m.ResponseDelayMaxMS); d > 0 {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}
```

After the existing mock-headers loop (`for k, v := range m.ResponseHeaders {...}`), add step headers (they win over the mock's own per key; the ContentType field set just below still wins over both — same precedence the main headers already have):

```go
	for k, v := range served.Headers {
		if service.IsReservedResponseHeader(k) {
			continue
		}
		w.Header().Set(k, v)
	}
```

Next to `w.Header().Set("X-Mockapi-Slug", m.Slug)`, add:

```go
	w.Header().Set("X-Mockapi-Variant", served.Variant)
```

Replace the status/body tail of `ServeHTTP`:

```go
	status := m.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
```

with:

```go
	status := served.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
```

and in the final `w.Write(...)` call change `m.ResponseBody` to `served.Body` (the `service.RenderResponseBodyForRequest(served.Body, &service.RequestData{...})` call keeps everything else identical).

- [ ] **Step 2: Wire in main.go**

In `cmd/server/main.go`, after `apiLimiter := ...` (line ~136), add:

```go
	seqCounter := repository.NewSeqCounter(rdb)
```

and change line ~171:

```go
	mockRouter := handler.NewMockRouter(mockSvc, logWriter, seqCounter)
```

- [ ] **Step 3: Build and run all tests**

Run: `go build ./... && go test ./... -race -count=1`
Expected: build clean, all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/handler/mock_router.go cmd/server/main.go
git commit -m "feat(router): serve flaky variants — error rate, sequences, delay jitter"
git push
```

---

### Task 7: API JSON fields

**Files:**
- Modify: `internal/handler/api.go`
- Test: `internal/handler/api_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/handler/api_test.go`:

```go
package handler

import (
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestCreateMockRequestToInputFlaky(t *testing.T) {
	req := createMockRequest{
		Method:             "get",
		ResponseDelayMaxMS: 2000,
		ErrorRatePct:       25,
		ErrorResponse:      &model.ResponseStep{Status: 503, Body: "boom"},
		ResponseSequence: []model.ResponseStep{
			{Status: 500, Body: "first"},
			{Status: 201, Body: "second", Headers: map[string]string{"X-Step": "2"}},
		},
	}
	in := req.toInput()
	if in.ResponseDelayMaxMS != 2000 || in.ErrorRatePct != 25 {
		t.Fatalf("scalar fields lost: %+v", in)
	}
	if in.ErrorResponse == nil || in.ErrorResponse.Status != 503 {
		t.Fatalf("error response lost: %+v", in.ErrorResponse)
	}
	if len(in.SequenceSteps) != 2 || in.SequenceSteps[1].Headers["X-Step"] != "2" {
		t.Fatalf("sequence lost: %+v", in.SequenceSteps)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -run TestCreateMockRequestToInputFlaky -v`
Expected: compile error — unknown fields on `createMockRequest`.

- [ ] **Step 3: Implement**

In `internal/handler/api.go`, extend `createMockRequest` (after `TTLSeconds`):

```go
	ResponseDelayMaxMS int                  `json:"response_delay_max_ms"`
	ErrorRatePct       int                  `json:"error_rate_pct"`
	ErrorResponse      *model.ResponseStep  `json:"error_response"`
	ResponseSequence   []model.ResponseStep `json:"response_sequence"`
```

Extend `toInput()` (after `TTL: ...`):

```go
		ResponseDelayMaxMS: req.ResponseDelayMaxMS,
		ErrorRatePct:       req.ErrorRatePct,
		ErrorResponse:      req.ErrorResponse,
		SequenceSteps:      req.ResponseSequence,
```

Extend `mockView` (after `"response_delay_ms"`):

```go
		"response_delay_max_ms": m.ResponseDelayMaxMS,
		"error_rate_pct":        m.ErrorRatePct,
		"error_response":        m.ErrorResponse,
		"response_sequence":     m.SequenceSteps,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/api.go internal/handler/api_test.go
git commit -m "feat(api): accept and echo flaky config on /api/mocks"
git push
```

---

### Task 8: HTML form parsing (TDD)

**Files:**
- Modify: `internal/handler/ui.go:200-233` (`readFormInput`)
- Test: `internal/handler/ui_test.go` (create)

Form contract (used by Task 9's template): `response_delay_max_ms`,
`error_rate_pct`, `error_status`, `error_body`, and parallel arrays
`seq_status[]`, `seq_body[]`, `seq_headers[]` where each `seq_headers[]`
entry is a textarea with one `Name: value` header per line.

- [ ] **Step 1: Write the failing tests**

Create `internal/handler/ui_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/handler/ -run 'TestReadFormInput|TestParseHeaderLines' -v`
Expected: compile error — `parseHeaderLines` undefined.

- [ ] **Step 3: Implement parsing**

In `internal/handler/ui.go`, inside `readFormInput` after the `headers` block, add:

```go
	delayMax, _ := strconv.Atoi(r.FormValue("response_delay_max_ms"))
	errRate, _ := strconv.Atoi(r.FormValue("error_rate_pct"))

	var errResp *model.ResponseStep
	if errRate > 0 {
		errStatus, _ := strconv.Atoi(r.FormValue("error_status"))
		errResp = &model.ResponseStep{Status: errStatus, Body: r.FormValue("error_body")}
	}

	// Sequence steps arrive as parallel arrays, same trick as headers.
	// Rows the user added but left fully empty are dropped.
	var steps []model.ResponseStep
	stStatus := r.Form["seq_status[]"]
	stBody := r.Form["seq_body[]"]
	stHeaders := r.Form["seq_headers[]"]
	for i := range stStatus {
		status, _ := strconv.Atoi(stStatus[i])
		body, hdrs := "", ""
		if i < len(stBody) {
			body = stBody[i]
		}
		if i < len(stHeaders) {
			hdrs = stHeaders[i]
		}
		if status == 0 && strings.TrimSpace(body) == "" && strings.TrimSpace(hdrs) == "" {
			continue
		}
		steps = append(steps, model.ResponseStep{
			Status:  status,
			Body:    body,
			Headers: parseHeaderLines(hdrs),
		})
	}
```

and extend the returned `model.MockInput` literal (after `ResponseDelayMS: delay,`):

```go
		ResponseDelayMaxMS: delayMax,
		ErrorRatePct:       errRate,
		ErrorResponse:      errResp,
		SequenceSteps:      steps,
```

After `readFormInput`, add:

```go
// parseHeaderLines turns a "Name: value" per-line textarea into a header
// map. Lines without a colon or without a name are skipped; nil is returned
// for an effectively empty input so the steps stay clean in storage.
func parseHeaderLines(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		name, value, ok := strings.Cut(strings.TrimRight(line, "\r"), ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/ui.go internal/handler/ui_test.go
git commit -m "feat(ui): parse flaky-simulation fields from the create form"
git push
```

---

### Task 9: Create-form UI + locales

**Files:**
- Modify: `web/templates/index.html`
- Modify: `locales/en.json`, `locales/ru.json`

No Go test — template work; verified by `go build` (templates are embedded
and parsed at startup) plus the manual happy path in Task 10. Reuse the
existing `kv-row`, `btn btn-ghost`, `form-hint`, `advanced` classes — no CSS
changes.

- [ ] **Step 1: Delay field grows a max input**

In `web/templates/index.html`, replace the delay label (lines ~183–187):

```html
      <label>
        <span class="label-text">{{ t "mock.create.delay" }}</span>
        <div class="kv-row">
          <input type="number" name="response_delay_ms" value="0" min="0" max="30000">
          <input type="number" name="response_delay_max_ms" min="0" max="30000"
                 placeholder='{{ t "mock.create.delay_max_placeholder" }}'>
        </div>
        <span class="form-hint">{{ t "mock.create.delay_jitter_hint" }}</span>
      </label>
```

- [ ] **Step 2: Flaky section**

After the headers `</details>` block (line ~214) and before the cURL-import
`<details>`, insert:

```html
    <details class="advanced">
      <summary>{{ t "mock.create.flaky.title" }}</summary>
      <p class="form-hint">{{ t "mock.create.flaky.hint" }}</p>

      <div class="form-grid">
        <label>
          <span class="label-text">{{ t "mock.create.flaky.error_rate" }}</span>
          <input type="number" name="error_rate_pct" x-model.number="errorRate" min="0" max="100" value="0">
          <span class="form-hint">{{ t "mock.create.flaky.error_rate_hint" }}</span>
        </label>
        <label x-show="errorRate > 0" x-cloak>
          <span class="label-text">{{ t "mock.create.flaky.error_status" }}</span>
          <input type="number" name="error_status" value="500" min="100" max="599">
        </label>
      </div>
      <label x-show="errorRate > 0" x-cloak>
        <span class="label-text">{{ t "mock.create.flaky.error_body" }}</span>
        <textarea name="error_body" rows="3" spellcheck="false"
                  placeholder='{{ t "mock.create.flaky.error_body_placeholder" }}'></textarea>
      </label>

      <p class="label-text">{{ t "mock.create.flaky.seq_title" }}</p>
      <p class="form-hint">{{ t "mock.create.flaky.seq_hint" }}</p>
      <template x-for="(st, i) in steps" :key="st.key">
        <div class="seq-step">
          <div class="kv-row">
            <span class="label-text" x-text="'{{ t "mock.create.flaky.step" }} ' + (i + 2)"></span>
            <input type="number" name="seq_status[]" :value="st.status"
                   @input="st.status = $event.target.value" min="100" max="599"
                   placeholder='{{ t "mock.create.status" }}'>
            <button type="button" class="btn btn-ghost" @click="moveStep(i, -1)"
                    :disabled="i === 0">↑</button>
            <button type="button" class="btn btn-ghost" @click="moveStep(i, 1)"
                    :disabled="i === steps.length - 1">↓</button>
            <button type="button" class="btn btn-ghost" @click="steps.splice(i, 1)">−</button>
          </div>
          <textarea name="seq_body[]" rows="3" spellcheck="false" :value="st.body"
                    @input="st.body = $event.target.value"
                    placeholder='{{ t "mock.create.flaky.step_body_placeholder" }}'></textarea>
          <textarea name="seq_headers[]" rows="2" spellcheck="false" :value="st.headers"
                    @input="st.headers = $event.target.value"
                    placeholder='{{ t "mock.create.flaky.step_headers_placeholder" }}'></textarea>
        </div>
      </template>
      <button type="button" class="btn btn-ghost" @click="addStep()"
              x-show="steps.length < 10">+ {{ t "mock.create.flaky.step_add" }}</button>
    </details>
```

- [ ] **Step 3: Alpine state**

In the `createForm()` return object (after `pathError: '',`), add:

```js
      errorRate: 0,
      steps: [],
      stepKey: 0,
      addStep() {
        if (this.steps.length >= 10) return;
        this.steps.push({ key: this.stepKey++, status: 500, body: '', headers: '' });
      },
      moveStep(i, d) {
        const j = i + d;
        if (j < 0 || j >= this.steps.length) return;
        [this.steps[i], this.steps[j]] = [this.steps[j], this.steps[i]];
      },
```

- [ ] **Step 4: Locale keys**

In `locales/en.json`, after the `"mock.create.delay_hint"` line, add:

```json
  "mock.create.delay_max_placeholder": "max (optional)",
  "mock.create.delay_jitter_hint": "Fixed delay, or set max for random jitter in the min–max range. 0–30000 ms.",
  "mock.create.flaky.title": "Flaky simulation",
  "mock.create.flaky.hint": "Make the mock behave like an unstable API: random errors and a response sequence that changes on every call. Tokens like {{faker.uuid}} work in every body.",
  "mock.create.flaky.error_rate": "Error rate (%)",
  "mock.create.flaky.error_rate_hint": "This share of requests gets the alternate response below. 0 turns it off.",
  "mock.create.flaky.error_status": "Error status",
  "mock.create.flaky.error_body": "Error body",
  "mock.create.flaky.error_body_placeholder": "{\"error\":\"injected by error rate\"}",
  "mock.create.flaky.seq_title": "Response sequence",
  "mock.create.flaky.seq_hint": "Responses are served in order — step 1 is the main response above — then loop. The position is shared by all callers.",
  "mock.create.flaky.step": "Step",
  "mock.create.flaky.step_body_placeholder": "Response body for this step",
  "mock.create.flaky.step_headers_placeholder": "Extra headers, one per line: X-Header: value",
  "mock.create.flaky.step_add": "Add step",
```

In `locales/ru.json`, same position:

```json
  "mock.create.delay_max_placeholder": "макс (необязательно)",
  "mock.create.delay_jitter_hint": "Фиксированная задержка, либо укажите максимум — и задержка станет случайной в диапазоне мин–макс. От 0 до 30000 мс.",
  "mock.create.flaky.title": "Симуляция нестабильности",
  "mock.create.flaky.hint": "Мок ведёт себя как нестабильный API: случайные ошибки и последовательность ответов, меняющаяся на каждый вызов. Токены вида {{faker.uuid}} работают в каждом теле.",
  "mock.create.flaky.error_rate": "Доля ошибок (%)",
  "mock.create.flaky.error_rate_hint": "Такая доля запросов получит альтернативный ответ ниже. 0 — выключено.",
  "mock.create.flaky.error_status": "Статус ошибки",
  "mock.create.flaky.error_body": "Тело ошибки",
  "mock.create.flaky.error_body_placeholder": "{\"error\":\"injected by error rate\"}",
  "mock.create.flaky.seq_title": "Последовательность ответов",
  "mock.create.flaky.seq_hint": "Ответы отдаются по порядку — шаг 1 это основной ответ выше — и затем по кругу. Позиция общая для всех вызывающих.",
  "mock.create.flaky.step": "Шаг",
  "mock.create.flaky.step_body_placeholder": "Тело ответа для этого шага",
  "mock.create.flaky.step_headers_placeholder": "Доп. заголовки, по одному в строке: X-Header: value",
  "mock.create.flaky.step_add": "Добавить шаг",
```

Note: in locale JSON the `{{faker.uuid}}` braces are plain text (locales are
not Go templates — see how `changelog.r0610.added1` already embeds them).

- [ ] **Step 5: Verify build + locale JSON**

Run: `go build ./... && go test ./... -race -count=1`
Expected: clean build, all PASS. (Template syntax itself is exercised at app
startup, which Task 10's manual pass covers.)

Run: `python3 -c "import json; json.load(open('locales/en.json')); json.load(open('locales/ru.json')); print('locales OK')"`
Expected: `locales OK`.

- [ ] **Step 6: Commit**

```bash
git add web/templates/index.html locales/en.json locales/ru.json
git commit -m "feat(ui): flaky-simulation section on the create form (en+ru)"
git push
```

---

### Task 10: Local verification (manual happy path)

**Files:** none (verification only)

- [ ] **Step 1: Start the stack**

Run: `docker compose up -d --build --wait`
Expected: app, postgres, redis healthy; the one-shot migration container applies `004_flaky` (check with `docker compose logs migrate | tail -5` — adjust service name per docker-compose.yml).

- [ ] **Step 2: Create a flaky mock via API**

```bash
curl -s -X POST http://localhost:8080/api/mocks \
  -H 'Content-Type: application/json' \
  -d '{
    "method": "GET",
    "response_status": 200,
    "content_type": "application/json",
    "response_body": "{\"step\":\"main\"}",
    "response_delay_ms": 100,
    "response_delay_max_ms": 400,
    "error_rate_pct": 0,
    "response_sequence": [
      {"status": 500, "body": "{\"step\":\"two\"}"},
      {"status": 201, "body": "{\"step\":\"three\"}", "headers": {"X-Step": "3"}}
    ]
  }' | tee /tmp/flaky.json
```

Expected: 201 JSON echoing `response_sequence` with 2 steps and `response_delay_max_ms: 400`.

- [ ] **Step 3: Watch it cycle**

```bash
SLUG=$(python3 -c "import json;print(json.load(open('/tmp/flaky.json'))['slug'])")
for i in 1 2 3 4 5 6; do
  curl -s -o /dev/null -w "%{http_code} variant=%header{x-mockapi-variant} time=%{time_total}\n" \
    http://localhost:8080/m/$SLUG
done
```

Expected: statuses cycle `200, 500, 201, 200, 500, 201`; variants cycle `seq-1/3, seq-2/3, seq-3/3, …`; every `time` between ~0.1 and ~0.5 s.

- [ ] **Step 4: Error rate check**

Create a second mock with `"error_rate_pct": 50, "error_response": {"status": 503, "body": "boom"}` and no sequence; hit it 20 times; expect a mix of `200/default` and `503/error` (roughly half, any split is fine — just both present).

- [ ] **Step 5: Browser check**

Open `http://localhost:8080/`, expand **Flaky simulation**, add two steps, set error rate 30%, submit; on the mock page curl the URL a few times and confirm cycling + occasional error. Confirm the form renders in RU as well (`?lang=ru`).

- [ ] **Step 6: Counter sharing check (Redis)**

```bash
curl -s -o /dev/null http://localhost:8080/m/$SLUG
docker compose exec redis redis-cli KEYS 'seq:*'
```

Expected: one `seq:<mock-id>` key with a TTL (`docker compose exec redis redis-cli TTL <key>` → ~604800).

No commit — verification only. If anything fails, fix forward in the task that owns the code.

---

### Task 11: Docs, changelog, backlog

**Files:**
- Modify: `API.md`
- Modify: `README.md`, `README.ru.md`
- Modify: `CHANGELOG.md`
- Modify: `web/templates/changelog.html`, `locales/en.json`, `locales/ru.json`
- Modify: `BACKLOG.md`

- [ ] **Step 1: API.md**

In the `POST /api/mocks` field table (after the `response_delay_ms` row), add:

```markdown
| `response_delay_max_ms` | int    | no  | Default `0` (off). When > `response_delay_ms`, each hit sleeps a uniform random duration in `[response_delay_ms, response_delay_max_ms]`. Max `30000`. |
| `error_rate_pct`        | int    | no  | Default `0` (off). 0–100. That share of requests answers with `error_response` instead of the normal response, rolled per request.                    |
| `error_response`        | object | when `error_rate_pct` > 0 | `{"status": int, "body": string}`. Headers and content-type are inherited from the mock. Status defaults to `500`.       |
| `response_sequence`     | array  | no  | Up to 10 extra steps `{"status": int, "body": string, "headers": object}` cycled after the main response: hit 1 → main response, hit 2 → step 1, … loop. The position is shared across all callers. |
```

Extend the request/response JSON examples in the same section with the new
fields (mirroring the curl example from Task 10 Step 2). In the
"Public mock router" section, after the `X-Mockapi-Slug` line, document:

```markdown
- `X-Mockapi-Variant: default | error | seq-<i>/<n>` says which flaky branch
  served this hit: the normal response, the error-rate alternate, or sequence
  step `i` of `n`.
```

and add a bullet next to the `response_delay_ms` honoring note:

```markdown
- When the mock has flaky settings, each hit first rolls the error rate, then
  advances the shared response sequence, then falls back to the main response;
  delay (fixed or jittered) applies to every variant.
```

- [ ] **Step 2: README feature bullets**

In `README.md`, the features list (~line 39) has
`- **Simulate slow APIs** — add a delay in milliseconds.` Replace with:

```markdown
- **Simulate slow APIs** — fixed delay, or a min–max range for random jitter.
- **Simulate flaky APIs** — an ordered response sequence cycled per call
  (1st → 200, 2nd → 500, repeat — classic retry testing) plus a configurable
  error rate that injects an alternate response for N% of requests.
```

Mirror the same two bullets in `README.ru.md` (find the matching
«Имитация медленных API» bullet):

```markdown
- **Имитация медленных API** — фиксированная задержка или случайный джиттер в диапазоне мин–макс.
- **Имитация нестабильных API** — последовательность ответов по кругу
  (1-й → 200, 2-й → 500, снова — классический тест ретраев) и настраиваемая
  доля ошибок, подменяющая ответ для N% запросов.
```

- [ ] **Step 3: CHANGELOG.md**

Under `## [Unreleased]`, add a new release section above the existing
`## [2026-06-10] — Request echo tokens`:

```markdown
## [2026-06-10] — Flaky-API simulation

### Added
- Response sequences: a mock can hold up to 10 extra ordered responses
  (status + body + headers each) cycled per hit after the main response —
  1st call → main, 2nd → step 1, … then loop. The position is a shared
  per-mock counter in Redis (in-process fallback), so all callers see one
  sequence. (M6.1)
- Error rate: N% of requests (0–100, rolled per request) answer with a
  configurable alternate status/body. Orthogonal to sequences: the error
  roll happens first and does not consume a sequence position. (M6.2)
- Delay jitter: `response_delay_max_ms` turns the fixed delay into a uniform
  random sleep in `[response_delay_ms, response_delay_max_ms]`; the old
  fixed field keeps working. (M6.3)
- `X-Mockapi-Variant` response header says which branch served each hit:
  `default`, `error`, or `seq-<i>/<n>`.
- All of it configurable from the create form (new “Flaky simulation”
  section, EN+RU) and `POST/PUT /api/mocks`.
```

- [ ] **Step 4: Public /changelog page**

In `locales/en.json`, retitle and extend the r0610 release:

- change `"changelog.r0610.title"` to `"2026-06-10 — Request echo & flaky simulation"`
- after `changelog.r0610.added1`, add:

```json
  "changelog.r0610.added2": "Flaky-API simulation: a mock can now hold an ordered <em>response sequence</em> cycled per hit (1st call → 200, 2nd → 500, repeat — classic retry-logic testing), inject an alternate error response for a configurable share of requests, and add random delay jitter via a min–max range. All three combine, and the <code>X-Mockapi-Variant</code> response header shows which branch served each hit.",
```

In `locales/ru.json`:

- change `"changelog.r0610.title"` to `"2026-06-10 — Эхо запроса и симуляция нестабильности"`
- add:

```json
  "changelog.r0610.added2": "Симуляция нестабильного API: мок может отдавать <em>последовательность ответов</em> по кругу (1-й вызов → 200, 2-й → 500, снова — классический тест retry-логики), подменять ответ ошибкой для настраиваемой доли запросов и добавлять случайный джиттер задержки в диапазоне мин–макс. Всё это сочетается, а заголовок <code>X-Mockapi-Variant</code> показывает, какая ветка обслужила вызов.",
```

In `web/templates/changelog.html`, in the r0610 article, add the second bullet:

```html
    <ul>
      <li>{{ tHTML "changelog.r0610.added1" }}</li>
      <li>{{ tHTML "changelog.r0610.added2" }}</li>
    </ul>
```

`LastUpdated` in `internal/handler/render.go` is already `"2026-06-10"` — no
bump needed. No new pages ⇒ no sitemap.xml changes.

In `internal/handler/seo.go` (`LLMsTxt`, "Key facts" list, line ~182),
replace the delay bullet:

```
- Configurable response delay up to 30 seconds (for testing slow APIs).
```

with:

```
- Configurable response delay up to 30 seconds — fixed, or a random min–max jitter range (for testing slow APIs).
- Flaky-API simulation: an ordered response sequence cycled per call (e.g. 1st → 200, 2nd → 500, repeat), plus a configurable error rate that injects an alternate response for N% of requests. The X-Mockapi-Variant response header shows which branch served each hit.
```

- [ ] **Step 5: BACKLOG.md**

Mark Epic 6 done. Change the epic header to:

```markdown
### Epic 6: Flaky-API simulation — **DONE** (2026-06-10)
```

and for each task: `#### M6.1 Response sequences — **DONE** (2026-06-10)`
(same for M6.2, M6.3), flipping every `- [ ]` in their AC lists to `- [x]`.

- [ ] **Step 6: Commit**

```bash
git add API.md README.md README.ru.md CHANGELOG.md web/templates/changelog.html locales/en.json locales/ru.json BACKLOG.md internal/handler/seo.go
git commit -m "docs: document flaky-API simulation — API.md, README, changelog (en+ru), backlog"
git push
```

---

### Task 12: Release

**Files:** none (deploy)

- [ ] **Step 1: Final full check**

Run: `make test && go vet ./... && go build ./...`
Expected: all PASS / clean.

- [ ] **Step 2: Deploy**

Run: `make deploy` (per DEPLOY.md this runs `ssh deploy@server "/var/www/quickmock/current/deploy.sh"` — docker mode pulls main, rebuilds, waits for healthy).
Expected: `==> Deploy complete: <short-sha>` where the sha matches local `git rev-parse --short HEAD`.

- [ ] **Step 3: Production smoke test**

```bash
curl -s https://quickmock.dev/healthz
# expect: ok / 200
curl -s -X POST https://quickmock.dev/api/mocks \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","response_body":"main","response_sequence":[{"status":500,"body":"second"}]}'
# expect: 201 with response_sequence echoed
# then hit /m/<slug> three times: 200 "main", 500 "second", 200 "main",
# with X-Mockapi-Variant seq-1/2, seq-2/2, seq-1/2.
```

Also open https://quickmock.dev/changelog and confirm the new entry renders
in EN and RU.

- [ ] **Step 4: Done**

Report release результат to the owner: what shipped, prod verification output, link to /changelog.
