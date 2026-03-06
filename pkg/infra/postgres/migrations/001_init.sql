-- SPDX-License-Identifier: GPL-2.0-only

-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id         BIGSERIAL PRIMARY KEY,
    username   TEXT UNIQUE NOT NULL,
    password   TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS channels (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    public     BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id BIGINT NOT NULL REFERENCES channels(id),
    user_id    BIGINT NOT NULL REFERENCES users(id),
    joined_at  TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (channel_id, user_id)
);

CREATE TABLE IF NOT EXISTS messages (
    id         BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES channels(id),
    user_id    BIGINT NOT NULL REFERENCES users(id),
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS read_cursors (
    channel_id           BIGINT NOT NULL REFERENCES channels(id),
    user_id              BIGINT NOT NULL REFERENCES users(id),
    last_read_message_id BIGINT NOT NULL REFERENCES messages(id),
    PRIMARY KEY (channel_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS read_cursors;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS users;
