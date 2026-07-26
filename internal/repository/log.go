package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// LogRepo is a thin Postgres adapter over `request_logs`.
type LogRepo struct {
	pool *pgxpool.Pool
}

func NewLogRepo(pool *pgxpool.Pool) *LogRepo { return &LogRepo{pool: pool} }

// Insert writes one row. A DB-side trigger trims each mock to its 100
// newest logs, so we don't need to delete here.
func (r *LogRepo) Insert(ctx context.Context, l *model.RequestLog) error {
	headers, err := json.Marshal(l.RequestHeaders)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO request_logs (mock_id, request_method, request_headers, request_body, request_ip)
		VALUES ($1,$2,$3,$4,$5)
	`,
		l.MockID, l.RequestMethod, headers, l.RequestBody, l.RequestIP,
	)
	return err
}

// LogFilter narrows the rows ListByMockID returns. The zero value matches
// every row — behavior identical to calling ListByMockID before this type
// existed.
type LogFilter struct {
	// Method, when non-empty, restricts results to logs with that exact
	// HTTP method (e.g. "POST"). Empty means any method.
	Method string
}

// ListByMockID returns up to `limit` newest logs for a mock. Optional `since`
// filters to rows created strictly after that timestamp; zero means no
// filter. `f` narrows results further (see LogFilter); its zero value is a
// no-op. Filtering happens in SQL — filter values are always passed as query
// parameters, never concatenated into the query text.
func (r *LogRepo) ListByMockID(ctx context.Context, mockID string, limit int, since time.Time, f LogFilter) ([]model.RequestLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT id, mock_id, request_method, request_headers, request_body, request_ip, created_at
		FROM request_logs
		WHERE mock_id = $1
	`
	args := []any{mockID}

	if !since.IsZero() {
		args = append(args, since)
		query += fmt.Sprintf(" AND created_at > $%d", len(args))
	}
	if f.Method != "" {
		args = append(args, f.Method)
		query += fmt.Sprintf(" AND request_method = $%d", len(args))
	}

	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.RequestLog
	for rows.Next() {
		var (
			l       model.RequestLog
			headers []byte
		)
		if err := rows.Scan(
			&l.ID, &l.MockID, &l.RequestMethod, &headers,
			&l.RequestBody, &l.RequestIP, &l.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(headers) > 0 {
			_ = json.Unmarshal(headers, &l.RequestHeaders)
		}
		if l.RequestHeaders == nil {
			l.RequestHeaders = map[string]string{}
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteByMockID purges every log row for a mock (clear-logs button).
func (r *LogRepo) DeleteByMockID(ctx context.Context, mockID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM request_logs WHERE mock_id = $1`, mockID)
	return err
}

// ResetCounter sets a mock's `request_count` back to zero. Used together
// with DeleteByMockID by the clear-logs handler.
func (r *LogRepo) ResetCounter(ctx context.Context, mockID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE mocks SET request_count = 0, last_request_at = NULL WHERE id = $1`, mockID)
	return err
}
