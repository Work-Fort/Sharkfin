-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS identities (
    id           TEXT PRIMARY KEY,
    username     TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT 'user',
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS channels (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    public     BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id  BIGINT NOT NULL REFERENCES channels(id),
    identity_id TEXT   NOT NULL REFERENCES identities(id),
    joined_at   TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (channel_id, identity_id)
);

CREATE TABLE IF NOT EXISTS messages (
    id          BIGSERIAL PRIMARY KEY,
    channel_id  BIGINT NOT NULL REFERENCES channels(id),
    identity_id TEXT   NOT NULL REFERENCES identities(id),
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS read_cursors (
    channel_id           BIGINT NOT NULL REFERENCES channels(id),
    identity_id          TEXT   NOT NULL REFERENCES identities(id),
    last_read_message_id BIGINT NOT NULL REFERENCES messages(id),
    PRIMARY KEY (channel_id, identity_id)
);

-- +goose Down
DROP TABLE IF EXISTS read_cursors;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS identities;
