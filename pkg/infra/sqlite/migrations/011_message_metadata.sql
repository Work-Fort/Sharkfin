-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
ALTER TABLE messages ADD COLUMN metadata TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions.
-- Leave column in place; it is nullable and ignored by old code.
