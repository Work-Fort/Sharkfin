// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// Compile-time check that Store implements domain.Store.
var _ domain.Store = (*Store)(nil)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// Store implements domain.Store backed by SQLite.
type Store struct {
	db *sql.DB
}

// Open opens an SQLite database and runs migrations.
func Open(path string) (*Store, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := sqldb.Exec("PRAGMA foreign_keys = ON"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if _, err := sqldb.Exec("PRAGMA journal_mode = WAL"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set journal mode: %w", err)
	}

	if _, err := sqldb.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// NORMAL sync is safe with WAL -- only WAL file writes skip fsync,
	// the main DB is still fsynced on checkpoint. ~12% write throughput gain.
	if _, err := sqldb.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}

	// Cap WAL size to prevent unbounded growth; 1000 pages ~ 4 MB triggers
	// an automatic checkpoint.
	if _, err := sqldb.Exec("PRAGMA wal_autocheckpoint = 1000"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set wal_autocheckpoint: %w", err)
	}

	// SQLite supports only one writer at a time. Limiting the pool to a
	// single connection avoids "database is locked" contention and ensures
	// PRAGMAs (foreign_keys, busy_timeout) apply to every query.
	sqldb.SetMaxOpenConns(1)

	if err := runMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: sqldb}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func runMigrations(db *sql.DB) error {
	fsys, err := fs.Sub(embedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
