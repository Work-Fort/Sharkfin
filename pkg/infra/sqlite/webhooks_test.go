// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterAndListWebhooks(t *testing.T) {
	s := newTestStore(t)

	// Need an identity first.
	s.UpsertIdentity("uuid-admin", "admin", "Admin", "user", "user")
	svcIdent, err := s.UpsertIdentity("uuid-svc", "flow-bot", "Flow", "service", "")
	require.NoError(t, err)

	id, err := s.RegisterWebhook(svcIdent.ID, "https://flow.internal/hook")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	hooks, err := s.GetActiveWebhooksForIdentity(svcIdent.ID)
	require.NoError(t, err)
	require.Len(t, hooks, 1)
	require.Equal(t, "https://flow.internal/hook", hooks[0].URL)
	require.Equal(t, id, hooks[0].ID)
}

func TestUnregisterWebhook(t *testing.T) {
	s := newTestStore(t)

	s.UpsertIdentity("uuid-admin", "admin", "Admin", "user", "user")
	svcIdent, _ := s.UpsertIdentity("uuid-svc", "flow-bot", "Flow", "service", "")

	s.RegisterWebhook(svcIdent.ID, "https://flow.internal/hook")

	hooks, _ := s.GetActiveWebhooksForIdentity(svcIdent.ID)
	require.Len(t, hooks, 1)

	err := s.UnregisterWebhook(svcIdent.ID, hooks[0].ID)
	require.NoError(t, err)

	hooks, _ = s.GetActiveWebhooksForIdentity(svcIdent.ID)
	require.Len(t, hooks, 0)
}
