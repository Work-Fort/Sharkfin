-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Add UNIQUE(identity_id, url) constraint to prevent duplicate webhook registrations.

-- +goose Up
CREATE UNIQUE INDEX idx_identity_webhooks_identity_url ON identity_webhooks(identity_id, url);

-- +goose Down
DROP INDEX IF EXISTS idx_identity_webhooks_identity_url;
