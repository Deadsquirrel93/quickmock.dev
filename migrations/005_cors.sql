-- 005_cors.sql — per-mock CORS preset toggle (M5.2).
--
-- When cors_enabled is true the serve handler emits a fixed, permissive,
-- credential-free CORS preset and answers OPTIONS preflight. The values are
-- server-owned constants; user-supplied Access-Control-* headers stay blocked
-- by reservedHeaders in internal/service/mock.go.

BEGIN;

ALTER TABLE mocks
    ADD COLUMN IF NOT EXISTS cors_enabled BOOLEAN NOT NULL DEFAULT false;

INSERT INTO schema_migrations (version) VALUES ('005_cors')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
