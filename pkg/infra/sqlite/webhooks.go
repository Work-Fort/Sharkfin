// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) RegisterWebhook(identityID, url, secret string) error {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("generate webhook id: %w", err)
	}
	id := hex.EncodeToString(buf)
	_, err := s.db.Exec(
		`INSERT INTO identity_webhooks (id, identity_id, url, secret) VALUES (?, ?, ?, ?)`,
		id, identityID, url, secret,
	)
	if err != nil {
		return fmt.Errorf("register webhook: %w", err)
	}
	return nil
}

func (s *Store) UnregisterWebhook(identityID, webhookID string) error {
	_, err := s.db.Exec(
		`DELETE FROM identity_webhooks WHERE id = ? AND identity_id = ?`,
		webhookID, identityID,
	)
	if err != nil {
		return fmt.Errorf("unregister webhook: %w", err)
	}
	return nil
}

func (s *Store) GetActiveWebhooksForIdentity(identityID string) ([]domain.IdentityWebhook, error) {
	rows, err := s.db.Query(
		`SELECT id, identity_id, url, secret FROM identity_webhooks WHERE identity_id = ? AND active = 1`,
		identityID,
	)
	if err != nil {
		return nil, fmt.Errorf("get webhooks: %w", err)
	}
	defer rows.Close()
	var hooks []domain.IdentityWebhook
	for rows.Next() {
		var h domain.IdentityWebhook
		if err := rows.Scan(&h.ID, &h.IdentityID, &h.URL, &h.Secret); err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		h.Active = true
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (s *Store) GetWebhooksForChannel(channelID int64) ([]domain.IdentityWebhook, error) {
	// Returns active webhooks for all service-type identities who are members of channelID.
	rows, err := s.db.Query(`
		SELECT iw.id, iw.identity_id, iw.url, iw.secret
		FROM identity_webhooks iw
		JOIN identities i ON iw.identity_id = i.id
		JOIN channel_members cm ON cm.identity_id = i.id AND cm.channel_id = ?
		WHERE iw.active = 1
		  AND i.type = 'service'
	`, channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel webhooks: %w", err)
	}
	defer rows.Close()
	var hooks []domain.IdentityWebhook
	for rows.Next() {
		var h domain.IdentityWebhook
		if err := rows.Scan(&h.ID, &h.IdentityID, &h.URL, &h.Secret); err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		h.Active = true
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}
