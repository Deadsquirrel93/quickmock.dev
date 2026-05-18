package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// ErrNotFound is returned when a row lookup matches nothing.
var ErrNotFound = errors.New("not found")

// MockRepo is a thin Postgres adapter over the `mocks` table.
type MockRepo struct {
	pool *pgxpool.Pool
}

func NewMockRepo(pool *pgxpool.Pool) *MockRepo { return &MockRepo{pool: pool} }

// Create inserts a new mock. The caller is responsible for slug uniqueness
// (the service layer retries on collision); a duplicate slug here returns
// the raw pgx error so the caller can detect the constraint violation.
func (r *MockRepo) Create(ctx context.Context, m *model.Mock) error {
	headers, err := json.Marshal(m.ResponseHeaders)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	var suffix *string
	if m.PathSuffix != "" {
		suffix = &m.PathSuffix
	}
	return r.pool.QueryRow(ctx, `
		INSERT INTO mocks (
			slug, name, method, response_body, response_status,
			response_headers, response_delay_ms, content_type,
			path_suffix, expires_at, creator_ip
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, created_at
	`,
		m.Slug, m.Name, string(m.Method), m.ResponseBody, m.ResponseStatus,
		headers, m.ResponseDelayMS, m.ContentType,
		suffix, m.ExpiresAt, m.CreatorIP,
	).Scan(&m.ID, &m.CreatedAt)
}

// BySlug fetches a mock by its public slug. Returns ErrNotFound if missing
// or already expired.
func (r *MockRepo) BySlug(ctx context.Context, slug string) (*model.Mock, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, slug, name, method, response_body, response_status,
		       response_headers, response_delay_ms, content_type,
		       path_suffix, expires_at, created_at, request_count,
		       last_request_at, creator_ip
		FROM mocks
		WHERE slug = $1
		  AND (expires_at IS NULL OR expires_at > now())
	`, slug)
	return scanMock(row)
}

// Update replaces every field on an existing mock. Returns ErrNotFound if
// the slug doesn't exist or has expired.
func (r *MockRepo) Update(ctx context.Context, m *model.Mock) error {
	headers, err := json.Marshal(m.ResponseHeaders)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	var suffix *string
	if m.PathSuffix != "" {
		suffix = &m.PathSuffix
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE mocks SET
			name              = $2,
			method            = $3,
			response_body     = $4,
			response_status   = $5,
			response_headers  = $6,
			response_delay_ms = $7,
			content_type      = $8,
			path_suffix       = $9,
			expires_at        = $10
		WHERE slug = $1
		  AND (expires_at IS NULL OR expires_at > now())
	`,
		m.Slug, m.Name, string(m.Method), m.ResponseBody, m.ResponseStatus,
		headers, m.ResponseDelayMS, m.ContentType, suffix, m.ExpiresAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBySlug removes a mock. Cascades to request_logs via FK.
func (r *MockRepo) DeleteBySlug(ctx context.Context, slug string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM mocks WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActiveByCreatorIP returns how many non-expired mocks were created
// from a given IP. Used to enforce QUICKMOCK_MAX_MOCKS.
func (r *MockRepo) CountActiveByCreatorIP(ctx context.Context, ip string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM mocks
		WHERE creator_ip = $1
		  AND (expires_at IS NULL OR expires_at > now())
	`, ip).Scan(&n)
	return n, err
}

// RecordHit atomically increments the request counter and updates the
// last-request timestamp. Best-effort: errors are logged but not surfaced.
func (r *MockRepo) RecordHit(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE mocks
		SET request_count = request_count + 1,
		    last_request_at = now()
		WHERE id = $1
	`, id)
	return err
}

// DeleteExpired removes mocks whose TTL has elapsed. Called periodically by
// a goroutine in main.go. Returns the number of deleted rows.
func (r *MockRepo) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM mocks
		WHERE expires_at IS NOT NULL AND expires_at <= now()
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SlugExists is a cheap existence check used by the service-layer slug
// generator. It includes expired rows on purpose: re-using a slug that
// belonged to a freshly-expired mock would surprise the previous owner.
func (r *MockRepo) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM mocks WHERE slug = $1)`,
		slug,
	).Scan(&exists)
	return exists, err
}

func scanMock(row pgx.Row) (*model.Mock, error) {
	var (
		m       model.Mock
		method  string
		headers []byte
		suffix  *string
	)
	err := row.Scan(
		&m.ID, &m.Slug, &m.Name, &method, &m.ResponseBody, &m.ResponseStatus,
		&headers, &m.ResponseDelayMS, &m.ContentType, &suffix,
		&m.ExpiresAt, &m.CreatedAt, &m.RequestCount, &m.LastRequestAt, &m.CreatorIP,
	)
	if suffix != nil {
		m.PathSuffix = *suffix
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Method = model.Method(method)
	if len(headers) > 0 {
		_ = json.Unmarshal(headers, &m.ResponseHeaders)
	}
	if m.ResponseHeaders == nil {
		m.ResponseHeaders = map[string]string{}
	}
	return &m, nil
}

