-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Per-identity webhook registrations.

-- +goose Up
CREATE TABLE identity_webhooks (
    id          TEXT PRIMARY KEY,
    identity_id TEXT NOT NULL REFERENCES identities(id),
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL DEFAULT '',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_identity_webhooks_identity_id ON identity_webhooks(identity_id);

-- +goose Down
DROP INDEX IF EXISTS idx_identity_webhooks_identity_id;
DROP TABLE IF EXISTS identity_webhooks;
