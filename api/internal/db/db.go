// Package db opens the SQLite database and applies the embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the database at path, enables WAL mode and
// foreign keys, and applies any pending migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := ensureFile(path); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
		"trusted_schema(0)",
	} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()

	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single connection serialises all access. This is a single-user tool;
	// it removes any chance of SQLITE_BUSY and makes read-modify-write
	// transactions trivially safe.
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	d.SetConnMaxLifetime(0)

	if err := d.PingContext(ctx); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	var fk int
	if err := d.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		_ = d.Close()
		return nil, errors.New("open database: foreign keys could not be enabled")
	}
	if err := migrate(ctx, d); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// ensureFile creates the database file with 0600 permissions if it does not
// exist, and tightens the permissions if it does. SQLite creates the -wal and
// -shm files with the same permissions as the main file.
func ensureFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	// Best effort: tighten a pre-existing directory we own (e.g. the volume).
	if dir != "." {
		_ = os.Chmod(dir, 0o700) // #nosec G302 -- 0700 is owner-only for a directory
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- path comes from operator configuration
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func migrate(ctx context.Context, d *sql.DB) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	entries, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		base := filepath.Base(name)
		num, _, ok := strings.Cut(base, "_")
		if !ok {
			return fmt.Errorf("migrate: bad migration file name %q", base)
		}
		version, err := strconv.Atoi(num)
		if err != nil {
			return fmt.Errorf("migrate: bad migration file name %q", base)
		}
		var applied int
		if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate %s: %w", base, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%s','now'))`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate %s: %w", base, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate %s: %w", base, err)
		}
	}
	return nil
}
