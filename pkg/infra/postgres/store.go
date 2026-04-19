// SPDX-License-Identifier: AGPL-3.0-or-later
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Compile-time check that Store implements domain.Store.
var _ domain.Store = (*Store)(nil)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// Store implements domain.Store backed by PostgreSQL.
type Store struct {
	db *sql.DB
}

// Open opens a PostgreSQL database and runs migrations.
// dsn should be a postgres:// or postgresql:// connection string.
func Open(dsn string) (*Store, error) {
	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqldb.SetMaxOpenConns(25)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(5 * time.Minute)

	if err := sqldb.Ping(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if err := runMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// Seed a default "general" channel on first run, matching the SQLite backend.
	var channelCount int
	_ = sqldb.QueryRow("SELECT COUNT(*) FROM channels").Scan(&channelCount)
	if channelCount == 0 {
		_, _ = sqldb.Exec("INSERT INTO channels (name, public, type) VALUES ('general', TRUE, 'channel')")
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
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
