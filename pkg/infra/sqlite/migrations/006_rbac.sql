-- SPDX-License-Identifier: GPL-2.0-only

-- +goose Up

-- RBAC tables
CREATE TABLE IF NOT EXISTS roles (
    name       TEXT PRIMARY KEY,
    built_in   BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
INSERT INTO roles (name, built_in) VALUES ('admin', 1);
INSERT INTO roles (name, built_in) VALUES ('user',  1);
INSERT INTO roles (name, built_in) VALUES ('agent', 1);

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

-- SQLite doesn't support DROP COLUMN before 3.35.0; recreate the users table
-- without the role and type columns.
CREATE TABLE users_backup AS SELECT id, username, password, created_at FROM users;
DROP TABLE users;
CREATE TABLE users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT UNIQUE NOT NULL,
    password   TEXT DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO users SELECT id, username, password, created_at FROM users_backup;
DROP TABLE users_backup;
