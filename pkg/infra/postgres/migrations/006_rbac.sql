-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up

-- RBAC tables
CREATE TABLE IF NOT EXISTS roles (
    name       TEXT PRIMARY KEY,
    built_in   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
    name TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role       TEXT NOT NULL REFERENCES roles(name),
    permission TEXT NOT NULL REFERENCES permissions(name),
    PRIMARY KEY (role, permission)
);

-- Add role and type columns to users.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
ALTER TABLE users ADD COLUMN type TEXT NOT NULL DEFAULT 'user';

-- Seed built-in roles.
INSERT INTO roles (name, built_in) VALUES ('admin', TRUE);
INSERT INTO roles (name, built_in) VALUES ('user',  TRUE);
INSERT INTO roles (name, built_in) VALUES ('agent', TRUE);

-- Seed permissions.
INSERT INTO permissions (name) VALUES ('send_message');
INSERT INTO permissions (name) VALUES ('create_channel');
INSERT INTO permissions (name) VALUES ('join_channel');
INSERT INTO permissions (name) VALUES ('invite_channel');
INSERT INTO permissions (name) VALUES ('history');
INSERT INTO permissions (name) VALUES ('unread_messages');
INSERT INTO permissions (name) VALUES ('unread_counts');
INSERT INTO permissions (name) VALUES ('mark_read');
INSERT INTO permissions (name) VALUES ('user_list');
INSERT INTO permissions (name) VALUES ('channel_list');
INSERT INTO permissions (name) VALUES ('dm_open');
INSERT INTO permissions (name) VALUES ('dm_list');
INSERT INTO permissions (name) VALUES ('manage_roles');

-- Admin gets all permissions.
INSERT INTO role_permissions (role, permission)
SELECT 'admin', name FROM permissions;

-- User and agent get everything except create_channel and manage_roles.
INSERT INTO role_permissions (role, permission)
SELECT 'user', name FROM permissions
WHERE name NOT IN ('create_channel', 'manage_roles');

INSERT INTO role_permissions (role, permission)
SELECT 'agent', name FROM permissions
WHERE name NOT IN ('create_channel', 'manage_roles');

-- +goose Down
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;

ALTER TABLE users DROP COLUMN role;
ALTER TABLE users DROP COLUMN type;
