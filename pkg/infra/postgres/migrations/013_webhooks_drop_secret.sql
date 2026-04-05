-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Remove unused secret column from identity_webhooks.

-- +goose Up
ALTER TABLE identity_webhooks DROP COLUMN secret;

-- +goose Down
ALTER TABLE identity_webhooks ADD COLUMN secret TEXT NOT NULL DEFAULT '';
