-- Add response_status to request_logs
ALTER TABLE request_logs ADD COLUMN response_status int;
