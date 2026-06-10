# Flaky-API Simulation (Epic 6) — Design

**Date:** 2026-06-10
**Status:** approved by owner (full scope, including `X-Mockapi-Variant` header)
**Backlog:** M6.1 Response sequences, M6.2 Error rate, M6.3 Delay jitter

## Goal

A mock today replays one fixed response with one fixed delay. Real APIs are
flaky: they answer differently on retries, fail intermittently, and have
variable latency. The README promises "reproduce a flaky edge case"; this
epic delivers it with three orthogonal per-mock settings:

1. **Response sequence** (M6.1) — an ordered list of responses cycled per
   hit, shared across all callers (1st call → step 1, 2nd → step 2, … loop).
2. **Error rate** (M6.2) — N% of requests answer with an alternate
   status/body, randomly per request (not time-bucketed).
3. **Delay jitter** (M6.3) — delay becomes a min–max range with a uniform
   random sleep; the old fixed field keeps working.

## Semantics (approved: orthogonal, with precedence)

All three can be enabled on one mock. Per request:

1. Roll `rand[0,100) < error_rate_pct` → serve the **error response**.
2. Else, if the mock has sequence steps → serve the **next sequence step**
   (per-mock shared counter; cycle = main response + extra steps).
3. Else → serve the **main response** (current behaviour).

Delay (fixed or jittered) applies to every variant. Every body — main, error,
step — goes through the existing `{{...}}` token renderer
(`service.RenderResponseBodyForRequest`).

The served variant is exposed as a debug header:
`X-Mockapi-Variant: default | error | seq-<i>/<n>` (1-based step index).

## Data model (migration `004_flaky.sql`)

Four new fields on `mocks`, following the existing single-table pattern
(no joins on the hot path, `Update` keeps replacing every field):

| Column                  | Type                      | Meaning |
|-------------------------|---------------------------|---------|
| `response_delay_max_ms` | `INT NOT NULL DEFAULT 0`  | If `> response_delay_ms`, sleep uniform random in `[response_delay_ms, response_delay_max_ms]`; else fixed delay as today. |
| `error_rate_pct`        | `SMALLINT NOT NULL DEFAULT 0` | 0–100. 0 = feature off. |
| `error_response`        | `JSONB NULL`              | `{"status": int, "body": string}`. Headers + content-type inherited from the mock. |
| `response_sequence`     | `JSONB NULL`              | Array of **extra** steps: `[{"status": int, "body": string, "headers": {string: string}}]`. The main response is implicitly step 1; cycle length = 1 + len(array). Max 10 extra steps. |

"Extra steps" (not "all steps") keeps old mocks and the default path free of
special cases: empty/NULL array ⇒ exactly today's behaviour.

Migration follows the `00N_name.sql` convention: `BEGIN; ALTER TABLE …;
INSERT INTO schema_migrations …; COMMIT;`.

## Model changes (`internal/model/mock.go`)

```go
// ResponseStep is one alternate response: a sequence step or the error-rate
// alternate (which ignores Headers).
type ResponseStep struct {
    Status  int
    Body    string
    Headers map[string]string
}
```

`Mock` and `MockInput` gain: `ResponseDelayMaxMS int`, `ErrorRatePct int`,
`ErrorResponse *ResponseStep`, `SequenceSteps []ResponseStep`.

## Validation (`MockService.validate`)

- `error_rate_pct` in 0–100; if > 0, `error_response` is required, its
  status in 100–599, body ≤ maxBody.
- If `error_rate_pct` == 0, `error_response` is dropped (normalised to NULL).
- `response_sequence`: ≤ 10 steps; each status in 100–599, body ≤ maxBody,
  header names validated by the existing `headerNameRegexp` and filtered
  through `cleanHeaders` (reserved-header stripping) — same rules as the
  mock's own headers.
- `response_delay_max_ms`: 0 (off) or ≥ `response_delay_ms`, ≤ 30000.

## Serve path (`internal/handler/mock_router.go`)

Variant selection is a pure function for testability:

```go
// pickVariant decides which response a hit gets.
// roll is rand[0,100); seqN is the 0-based counter value (used only when
// the mock has steps).
func pickVariant(m *model.Mock, roll int, seqN uint64) (variant string, status int, body string, headers map[string]string)
```

Router flow: resolve mock → method check → log submit (unchanged) →
`pickVariant` (counter fetched only when steps exist) → sleep (jitter-aware,
still select-ing on `r.Context().Done()`) → write headers (mock headers
first, step headers override per-key, then the protective headers as today) →
`X-Mockapi-Variant` → render body tokens → write.

Step headers go through `IsReservedResponseHeader` on the serve path too
(same defence-in-depth as mock headers).

## Sequence counter (`internal/repository/seq.go`)

`SeqCounter` with Redis `INCR seq:<mockID>` pipelined with `EXPIRE` (sliding
7-day TTL — keys self-clean after a mock dies; position is "cheap to lose"
state per ARCHITECTURE.md). On Redis error, fall back to an in-process
`sync.Map` of `*atomic.Uint64` keyed by mock ID so cycling keeps working
within the instance. Returns the 0-based position.

Wiring: `NewMockRouter(svc, logWriter, seqCounter)`; `rdb` already exists in
`cmd/server/main.go`.

## UI (`web/templates/index.html`, Alpine.js)

New `<details class="advanced">` section "Flaky simulation":

- **Jitter:** a second number input "max delay (ms, optional)" next to the
  existing delay field (0/empty = off).
- **Error rate:** number input 0–100 (%), alternate status input, alternate
  body textarea. Visible hint that 0 disables it.
- **Sequence:** Alpine `x-for` list of steps (status input, body textarea,
  optional per-step headers using the same name/value row pattern as the
  existing headers editor). Buttons: ↑ / ↓ / remove per step, "+ Add step".
  A static label explains "Step 1 is the main response above"; added steps
  are numbered from 2.

All strings via `t` keys in `locales/en.json` + `locales/ru.json`.
(Note: the UI has no edit form — only create. `PUT /api/mocks/:id` picks the
new fields up through the shared request struct; no extra UI work.)

## API (`internal/handler/api.go`)

`createMockRequest` (used by POST and PUT) gains:

```json
{
  "response_delay_max_ms": 0,
  "error_rate_pct": 0,
  "error_response": {"status": 503, "body": "..."},
  "response_sequence": [{"status": 500, "body": "...", "headers": {}}]
}
```

`mockView` echoes the same fields back.

## Testing

- `pickVariant`: error-rate boundaries (0, 100, roll edges), cycling order
  with and without steps, precedence error > sequence > default.
- Validation: rate range, error-response required when rate > 0, step count
  cap, per-step body/status/header rules, jitter min/max rules.
- Jitter: sleep duration within [min, max] (clock-free: assert computed
  duration, not wall time).
- `SeqCounter`: Redis path (miniredis or skip if no test Redis — follow
  existing repo test patterns), in-process fallback cycles correctly.
- Handler-level: sequence cycles across requests with a fake counter;
  `X-Mockapi-Variant` set correctly.

## Release checklist

1. `make test` + manual happy path in browser (create flaky mock, curl it).
2. CHANGELOG.md + public `/changelog` page entry (template + en/ru locales
   + LastUpdated bump) — per project convention.
3. BACKLOG.md: mark M6.1–M6.3 done with date.
4. API.md: document the new request/response fields and the
   `X-Mockapi-Variant` header.
5. Conventional commits to `main`, push, deploy via `deploy.sh`.

## Out of scope

- CORS preset (M5.2), SSE inspector (M5.3), export/import (M5.4).
- Per-step delays (delay stays mock-level).
- Conditional responses by header/body match (Phase 2).
