// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"regexp"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

// resolveMentions extracts @-patterns from the message body, resolves
// usernames directly and expands mention groups to their members.
// Invalid usernames and unknown groups are silently ignored.
func resolveMentions(store domain.Store, body string) ([]string, []string) {
	seen := make(map[string]bool)
	seenIDs := make(map[string]bool)
	var identityIDs []string
	var usernames []string
	var unresolved []string

	// Extract all @candidates from body.
	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if seen[u] {
			continue
		}
		seen[u] = true

		identity, err := store.GetIdentityByUsername(u)
		if err != nil {
			unresolved = append(unresolved, u)
			continue
		}
		seenIDs[identity.ID] = true
		identityIDs = append(identityIDs, identity.ID)
		usernames = append(usernames, identity.Username)
	}

	// Expand unresolved candidates as group slugs.
	if len(unresolved) > 0 {
		expanded, err := store.ExpandMentionGroups(unresolved)
		if err == nil {
			for slug, memberIDs := range expanded {
				usernames = append(usernames, slug)
				for _, id := range memberIDs {
					if !seenIDs[id] {
						seenIDs[id] = true
						identityIDs = append(identityIDs, id)
					}
				}
			}
		}
	}

	return identityIDs, usernames
}
