// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"regexp"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

// resolveMentions extracts @username patterns from the message body,
// merges with any explicitly provided usernames, deduplicates, and
// resolves each against the database. Invalid usernames are silently ignored.
func resolveMentions(database *db.DB, body string, explicit []string) ([]int64, []string) {
	seen := make(map[string]bool)
	var userIDs []int64
	var usernames []string

	// Collect all candidate usernames (explicit first, then body-extracted)
	var candidates []string
	for _, u := range explicit {
		if !seen[u] {
			seen[u] = true
			candidates = append(candidates, u)
		}
	}
	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if !seen[u] {
			seen[u] = true
			candidates = append(candidates, u)
		}
	}

	// Resolve each candidate, silently skip invalid ones
	for _, uname := range candidates {
		u, err := database.GetUserByUsername(uname)
		if err != nil {
			continue
		}
		userIDs = append(userIDs, u.ID)
		usernames = append(usernames, u.Username)
	}

	return userIDs, usernames
}
