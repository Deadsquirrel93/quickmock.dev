// Package repository contains thin SQL/Redis adapters. No business logic.
package repository

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations applies every .sql file in `dir` of `fsys` in lexical order.
// Each file's name (without extension) is recorded in schema_migrations and
// skipped on subsequent runs.
//
// Files are expected to be self-contained transactions — they may, but need
// not, wrap themselves in BEGIN/COMMIT. RunMigrations does not implicitly
// wrap them, so a multi-statement file that fails mid-way will leave the DB
// in a partial state. For MVP this is fine; we only ship 001_init.sql.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	// Bootstrap the migrations table. 001_init.sql also creates it, but if
	// a future migration is applied to an existing schema-less DB we still
	// want the bookkeeping to work.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")

		var applied bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
			version,
		).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}

		fp := name
		if dir != "" && dir != "." {
			fp = path.Join(dir, name)
		}
		sqlBytes, err := fs.ReadFile(fsys, fp)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`,
			version,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}

	return nil
}
