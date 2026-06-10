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
