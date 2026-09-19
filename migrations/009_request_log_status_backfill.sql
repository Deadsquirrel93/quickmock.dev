-- 009_request_log_status_backfill.sql — repair databases that already ran the
-- first cut of 008, which added response_status as a plain nullable column.
--
-- RunMigrations skips a version once it is recorded, so fixing 008 in place
-- only helps fresh databases; an environment that already applied it keeps a
-- nullable column full of NULLs for every request logged before the deploy,
-- and every ListByMockID call over those rows fails. This migration is a
-- no-op on a database that got the corrected 008.

BEGIN;

UPDATE request_logs SET response_status = 0 WHERE response_status IS NULL;

ALTER TABLE request_logs ALTER COLUMN response_status SET DEFAULT 0;
ALTER TABLE request_logs ALTER COLUMN response_status SET NOT NULL;

INSERT INTO schema_migrations (version) VALUES ('009_request_log_status_backfill')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
