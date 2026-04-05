-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
ALTER TABLE messages ADD COLUMN metadata TEXT;

-- +goose Down
ALTER TABLE messages DROP COLUMN IF EXISTS metadata;
