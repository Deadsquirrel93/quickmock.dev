-- 006_admin_token.sql — per-mock admin token for authorized mutations.
--
-- admin_token_hash stores the SHA-256 hash (hex) of a one-time-shown plain
-- token (`qm_<64 hex>`, see internal/service/token.go). NULL means legacy:
-- the mock was created before this feature and its mutations stay
-- slug-only until it expires (≤ 30 days), matching pre-feature behavior.

BEGIN;

ALTER TABLE mocks
    ADD COLUMN IF NOT EXISTS admin_token_hash TEXT;

INSERT INTO schema_migrations (version) VALUES ('006_admin_token')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
