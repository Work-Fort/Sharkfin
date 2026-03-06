-- SPDX-License-Identifier: GPL-2.0-only

-- +goose Up
ALTER TABLE channels ADD COLUMN type TEXT NOT NULL DEFAULT 'channel';

-- Tag existing private channels with exactly 2 members as DMs.
UPDATE channels SET type = 'dm'
WHERE public = FALSE
  AND id IN (
    SELECT channel_id FROM channel_members
    GROUP BY channel_id HAVING COUNT(*) = 2
  );

-- +goose Down
ALTER TABLE channels DROP COLUMN type;
