-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS message_mentions (
    message_id BIGINT NOT NULL REFERENCES messages(id),
    user_id    BIGINT NOT NULL REFERENCES users(id),
    PRIMARY KEY (message_id, user_id)
);

ALTER TABLE messages ADD COLUMN thread_id BIGINT REFERENCES messages(id);

CREATE INDEX idx_messages_thread_id ON messages(thread_id);
CREATE INDEX idx_message_mentions_user_id ON message_mentions(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_message_mentions_user_id;
DROP INDEX IF EXISTS idx_messages_thread_id;
DROP TABLE IF EXISTS message_mentions;
ALTER TABLE messages DROP COLUMN thread_id;
