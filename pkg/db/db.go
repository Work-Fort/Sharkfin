// SPDX-License-Identifier: GPL-2.0-only
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps an SQLite database connection.
type DB struct {
	db *sql.DB
}

// Open opens an SQLite database and runs migrations.
func Open(path string) (*DB, error) {
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

	d := &DB{db: sqldb}
	if err := d.migrate(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		username   TEXT UNIQUE NOT NULL,
		password   TEXT DEFAULT '',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS channels (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		public     BOOLEAN DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS channel_members (
		channel_id INTEGER NOT NULL REFERENCES channels(id),
		user_id    INTEGER NOT NULL REFERENCES users(id),
		joined_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (channel_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_id INTEGER NOT NULL REFERENCES channels(id),
		user_id    INTEGER NOT NULL REFERENCES users(id),
		body       TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS read_cursors (
		channel_id           INTEGER NOT NULL REFERENCES channels(id),
		user_id              INTEGER NOT NULL REFERENCES users(id),
		last_read_message_id INTEGER NOT NULL REFERENCES messages(id),
		PRIMARY KEY (channel_id, user_id)
	);
	`
	_, err := d.db.Exec(schema)
	return err
}
