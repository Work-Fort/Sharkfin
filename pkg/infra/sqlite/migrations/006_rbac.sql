-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up

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

ALTER TABLE identities ADD COLUMN role TEXT NOT NULL DEFAULT 'user';

INSERT INTO roles (name, built_in) VALUES ('admin', 1);
INSERT INTO roles (name, built_in) VALUES ('user',  1);
INSERT INTO roles (name, built_in) VALUES ('agent', 1);

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

INSERT INTO role_permissions (role, permission)
SELECT 'admin', name FROM permissions;

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

CREATE TABLE identities_backup AS SELECT id, username, display_name, type, created_at FROM identities;
DROP TABLE identities;
CREATE TABLE identities (
    id           TEXT PRIMARY KEY,
    username     TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT 'user',
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO identities SELECT id, username, display_name, type, created_at FROM identities_backup;
DROP TABLE identities_backup;
