-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS mention_groups (
    id         SERIAL PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES identities(id),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mention_group_members (
    group_id    INTEGER NOT NULL REFERENCES mention_groups(id) ON DELETE CASCADE,
    identity_id TEXT    NOT NULL REFERENCES identities(id),
    PRIMARY KEY (group_id, identity_id)
);

CREATE INDEX idx_mention_group_members_identity ON mention_group_members(identity_id);

-- +goose Down
DROP TABLE IF EXISTS mention_group_members;
DROP TABLE IF EXISTS mention_groups;
