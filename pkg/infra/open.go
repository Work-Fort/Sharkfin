// SPDX-License-Identifier: AGPL-3.0-or-later
package infra

import (
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra/postgres"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

// Open auto-detects the database backend from the DSN and returns a Store.
//
// DSN formats:
//   - postgres://... or postgresql://...  → PostgreSQL
//   - Any file path or :memory:           → SQLite
func Open(dsn string) (domain.Store, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return postgres.Open(dsn)
	}
	return sqlite.Open(dsn)
}
