-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
ALTER TABLE channels ADD COLUMN type TEXT NOT NULL DEFAULT 'channel';

-- Tag existing private channels with exactly 2 members as DMs.
UPDATE channels SET type = 'dm'
WHERE public = 0
  AND id IN (
    SELECT channel_id FROM channel_members
    GROUP BY channel_id HAVING COUNT(*) = 2
  );

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0; recreate table.
CREATE TABLE channels_backup AS SELECT id, name, public, created_at FROM channels;
DROP TABLE channels;
CREATE TABLE channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    public     BOOLEAN DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO channels SELECT id, name, public, created_at FROM channels_backup;
DROP TABLE channels_backup;
CREATE UNIQUE INDEX idx_channels_name ON channels(name);
