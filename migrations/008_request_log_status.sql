-- 008_request_log_status.sql — record the status code each mock replied with,
-- so the inspector can show it and the log list can be filtered by it.
--
-- NOT NULL DEFAULT 0 is load-bearing: LogRepo.ListByMockID scans this column
-- into a plain Go int, and pgx refuses to scan NULL into one ("cannot scan
-- NULL into *int"), which would fail the whole query the moment it met a row
-- written before this migration. 0 is the "unknown" sentinel the inspector
-- renders as "?" — see web/templates/partials/logs_inner.html.

BEGIN;

ALTER TABLE request_logs
    ADD COLUMN IF NOT EXISTS response_status INT NOT NULL DEFAULT 0;

INSERT INTO schema_migrations (version) VALUES ('008_request_log_status')
    ON CONFLICT (version) DO NOTHING;

COMMIT;
