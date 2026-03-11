// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"regexp"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

// resolveMentions extracts @username patterns from the message body,
// deduplicates, and resolves each against the database. Invalid usernames
// are silently ignored.
func resolveMentions(store domain.UserStore, body string) ([]int64, []string) {
	seen := make(map[string]bool)
	var userIDs []int64
	var usernames []string

	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if seen[u] {
			continue
		}
		seen[u] = true

		user, err := store.GetUserByUsername(u)
		if err != nil {
			continue
		}
		userIDs = append(userIDs, user.ID)
		usernames = append(usernames, user.Username)
	}

	return userIDs, usernames
}
