-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up

-- Remove duplicate channels, keeping the oldest (lowest id) for each name.
-- First, move members from duplicate channels to the kept channel.
INSERT INTO channel_members (channel_id, user_id, joined_at)
SELECT keeper.id, cm.user_id, cm.joined_at
FROM channel_members cm
JOIN channels dup ON cm.channel_id = dup.id
JOIN (SELECT MIN(id) AS id, name FROM channels GROUP BY name) keeper ON dup.name = keeper.name
WHERE dup.id != keeper.id
ON CONFLICT DO NOTHING;

-- Re-point messages from duplicate channels to the kept channel.
UPDATE messages SET channel_id = (
    SELECT MIN(c2.id) FROM channels c2 WHERE c2.name = (SELECT c3.name FROM channels c3 WHERE c3.id = messages.channel_id)
)
WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Re-point read_cursors from duplicate channels to the kept channel.
INSERT INTO read_cursors (channel_id, user_id, last_read_message_id)
SELECT (SELECT MIN(c2.id) FROM channels c2 WHERE c2.name = (SELECT c3.name FROM channels c3 WHERE c3.id = rc.channel_id)),
       rc.user_id, rc.last_read_message_id
FROM read_cursors rc
WHERE rc.channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name)
ON CONFLICT (channel_id, user_id)
DO UPDATE SET last_read_message_id = GREATEST(read_cursors.last_read_message_id, EXCLUDED.last_read_message_id);

DELETE FROM read_cursors WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Delete members of duplicate channels.
DELETE FROM channel_members WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Delete duplicate channels.
DELETE FROM channels WHERE id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Now safe to add the unique index.
CREATE UNIQUE INDEX idx_channels_name ON channels(name);

-- +goose Down
DROP INDEX IF EXISTS idx_channels_name;
