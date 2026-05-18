-- 002_path_suffix.sql — let users append a readable suffix to their mock URL.
--
-- The suffix is cosmetic: anything under /m/<slug>/* still routes to the
-- mock. Storing it lets us render the canonical URL (the one the user typed)
-- in the UI, cURL snippet, and code generation.

BEGIN;

ALTER TABLE mocks
    ADD COLUMN IF NOT EXISTS path_suffix TEXT;

ALTER TABLE mocks
    DROP CONSTRAINT IF EXISTS mocks_path_suffix_check;

ALTER TABLE mocks
    ADD CONSTRAINT mocks_path_suffix_check CHECK (
        path_suffix IS NULL
        OR (
            length(path_suffix) BETWEEN 1 AND 255
            AND path_suffix !~ '^/'
            AND path_suffix !~ '/$'
            AND path_suffix !~ '//'
        )
    );

INSERT INTO schema_migrations (version) VALUES ('002_path_suffix')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
