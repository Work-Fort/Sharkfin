-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
INSERT INTO roles (name, built_in) VALUES ('bot', 1);

INSERT INTO role_permissions (role, permission) VALUES ('bot', 'send_message');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'join_channel');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'channel_list');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'history');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'unread_messages');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'unread_counts');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'mark_read');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'create_channel');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'dm_list');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'dm_open');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'user_list');

-- +goose Down
DELETE FROM role_permissions WHERE role = 'bot';
DELETE FROM roles WHERE name = 'bot';
