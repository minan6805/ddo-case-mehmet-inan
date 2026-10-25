package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies ordered SQL files atomically. An advisory lock serializes API starts.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420181)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
    )`); err != nil {
		return err
	}
	entries, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		var existing string
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE name=$1`, name).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			if err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name=$1`, name).Scan(&existing); err != nil {
				return err
			}
			if existing != checksum {
				return fmt.Errorf("migration changed after application: %s", name)
			}
			continue
		}
		// No parameters: pgx uses simple protocol for multi-statement SQL migrations.
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name, checksum) VALUES ($1,$2)`, name, checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
