-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS mention_groups (
    id         SERIAL PRIMARY KEY,
    slug       TEXT   NOT NULL UNIQUE,
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mention_group_members (
    group_id INTEGER NOT NULL REFERENCES mention_groups(id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users(id),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_mention_group_members_user ON mention_group_members(user_id);

-- +goose Down
DROP TABLE IF EXISTS mention_group_members;
DROP TABLE IF EXISTS mention_groups;
