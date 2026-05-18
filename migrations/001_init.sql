-- 001_init.sql — initial schema for Mock API.
-- Applied by `quickmock migrate`. Idempotent.

BEGIN;

CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- for gen_random_uuid()

-- ---------------------------------------------------------------------------
-- mocks
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS mocks (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug               TEXT        NOT NULL UNIQUE,
    name               TEXT,
    method             TEXT        NOT NULL,
    response_body      TEXT        NOT NULL DEFAULT '',
    response_status    INTEGER     NOT NULL DEFAULT 200,
    response_headers   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    response_delay_ms  INTEGER     NOT NULL DEFAULT 0,
    content_type       TEXT        NOT NULL DEFAULT 'text/plain',
    expires_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_count      BIGINT      NOT NULL DEFAULT 0,
    last_request_at    TIMESTAMPTZ,
    creator_ip         TEXT,

    CONSTRAINT mocks_method_check
        CHECK (method IN ('GET','POST','PUT','PATCH','DELETE','ANY')),
    CONSTRAINT mocks_status_check
        CHECK (response_status BETWEEN 100 AND 599),
    CONSTRAINT mocks_delay_check
        CHECK (response_delay_ms BETWEEN 0 AND 30000),
    CONSTRAINT mocks_body_size_check
        CHECK (octet_length(response_body) <= 524288)
);

-- Lookup by slug — hot path for /m/:slug.
CREATE UNIQUE INDEX IF NOT EXISTS mocks_slug_idx ON mocks (slug);

-- Used by the hourly expiration job.
CREATE INDEX IF NOT EXISTS mocks_expires_at_idx
    ON mocks (expires_at)
    WHERE expires_at IS NOT NULL;

-- Used for "active mocks per IP" rate limit.
CREATE INDEX IF NOT EXISTS mocks_creator_ip_idx ON mocks (creator_ip);

-- ---------------------------------------------------------------------------
-- request_logs
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS request_logs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    mock_id         UUID        NOT NULL REFERENCES mocks(id) ON DELETE CASCADE,
    request_method  TEXT        NOT NULL,
    request_headers JSONB       NOT NULL DEFAULT '{}'::jsonb,
    request_body    TEXT        NOT NULL DEFAULT '',
    request_ip      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Used for "latest N requests per mock".
CREATE INDEX IF NOT EXISTS request_logs_mock_created_idx
    ON request_logs (mock_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Cap request_logs to 100 newest per mock.
--
-- A trigger keeps the table tidy without a separate cleanup job.
-- For very high write volume this can be replaced with a periodic batch
-- delete; for MVP a trigger is simpler.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION trim_request_logs() RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM request_logs
    WHERE id IN (
        SELECT id FROM request_logs
        WHERE mock_id = NEW.mock_id
        ORDER BY created_at DESC
        OFFSET 100
    );
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS request_logs_trim_trigger ON request_logs;
CREATE TRIGGER request_logs_trim_trigger
    AFTER INSERT ON request_logs
    FOR EACH ROW
    EXECUTE FUNCTION trim_request_logs();

-- ---------------------------------------------------------------------------
-- schema_migrations — track applied migrations.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT        PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version) VALUES ('001_init')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
