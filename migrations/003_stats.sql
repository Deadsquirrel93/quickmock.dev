-- 003_stats.sql — lifetime counters for the home page.
--
-- Why separate from `mocks.request_count`: rows in `mocks` get deleted when
-- the mock expires or the user removes it, taking their counts with them.
-- We want the public counter on the home page to be a real lifetime total,
-- so we keep it in its own single-row(s) table that never decrements.
--
-- Keys are free-form so future metrics ("languages_loaded", whatever) can
-- live in the same place.

BEGIN;

CREATE TABLE IF NOT EXISTS stats (
    key   TEXT   PRIMARY KEY,
    value BIGINT NOT NULL DEFAULT 0
);

INSERT INTO stats (key, value) VALUES
    ('mocks_created',    0),
    ('requests_served',  0)
ON CONFLICT (key) DO NOTHING;

INSERT INTO schema_migrations (version) VALUES ('003_stats')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
