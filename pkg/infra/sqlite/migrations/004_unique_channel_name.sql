-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up

INSERT OR IGNORE INTO channel_members (channel_id, identity_id, joined_at)
SELECT keeper.id, cm.identity_id, cm.joined_at
FROM channel_members cm
JOIN channels dup ON cm.channel_id = dup.id
JOIN (SELECT MIN(id) AS id, name FROM channels GROUP BY name) keeper ON dup.name = keeper.name
WHERE dup.id != keeper.id;

UPDATE messages SET channel_id = (
    SELECT MIN(c2.id) FROM channels c2 WHERE c2.name = (SELECT c3.name FROM channels c3 WHERE c3.id = messages.channel_id)
)
WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

INSERT OR REPLACE INTO read_cursors (channel_id, identity_id, last_read_message_id)
SELECT (SELECT MIN(c2.id) FROM channels c2 WHERE c2.name = (SELECT c3.name FROM channels c3 WHERE c3.id = rc.channel_id)),
       rc.identity_id, rc.last_read_message_id
FROM read_cursors rc
WHERE rc.channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

DELETE FROM read_cursors WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

DELETE FROM channel_members WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

DELETE FROM channels WHERE id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

CREATE UNIQUE INDEX idx_channels_name ON channels(name);

-- +goose Down
DROP INDEX IF EXISTS idx_channels_name;
