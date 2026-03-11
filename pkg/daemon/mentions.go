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
func resolveMentions(store domain.Store, body string) ([]int64, []string) {
	seen := make(map[string]bool)
	seenIDs := make(map[int64]bool)
	var userIDs []int64
	var usernames []string
	var unresolved []string

	// Extract all @candidates from body.
	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if seen[u] {
			continue
		}
		seen[u] = true

		user, err := store.GetUserByUsername(u)
		if err != nil {
			unresolved = append(unresolved, u)
			continue
		}
		seenIDs[user.ID] = true
		userIDs = append(userIDs, user.ID)
		usernames = append(usernames, user.Username)
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
						userIDs = append(userIDs, id)
					}
				}
			}
		}
	}

	return userIDs, usernames
}
