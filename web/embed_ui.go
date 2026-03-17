//go:build ui

// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import "embed"

// Dist holds the Vite build output. Built via:
//
//	cd web && pnpm build
//	go build -tags ui
//
//go:embed all:dist
var Dist embed.FS
