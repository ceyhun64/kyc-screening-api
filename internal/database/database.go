// Package database opens the PostgreSQL connection and applies migrations.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	_ "github.com/lib/pq" // registers the "postgres" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to PostgreSQL and waits until it answers, which helps when
// the database container is still starting (e.g. with docker compose).
func Open(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			return db, nil
		}
		if attempt == 10 {
			db.Close()
			return nil, fmt.Errorf("ping database after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Migrate applies the SQL files in migrations/ in name order. Applied files
// are recorded in schema_migrations, so each file runs only once.
//
// This is intentionally minimal. A production service would use a tool such
// as golang-migrate or goose, and take a lock so that two instances starting
// at the same time don't migrate in parallel.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, file := range files {
		if err := applyOne(ctx, db, file); err != nil {
			return fmt.Errorf("migration %s: %w", file, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, file string) error {
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, file,
	).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	content, err := migrations.ReadFile(file)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, file); err != nil {
		return err
	}
	return tx.Commit()
}
