-- 007_advanced_mocks.sql — deterministic variants, conditional rules,
-- multi-route workspaces, and inspector privacy controls.

BEGIN;

ALTER TABLE mocks
    ADD COLUMN IF NOT EXISTS response_variants JSONB,
    ADD COLUMN IF NOT EXISTS response_rules JSONB,
    ADD COLUMN IF NOT EXISTS routes JSONB,
    ADD COLUMN IF NOT EXISTS logs_public BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS capture_body BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS capture_ip BOOLEAN NOT NULL DEFAULT true;

INSERT INTO schema_migrations (version) VALUES ('007_advanced_mocks')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
