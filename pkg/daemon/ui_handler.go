// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"io/fs"
	"net/http"
)

// registerUIRoutes adds the /ui/health probe and static file serving.
// If uiDir is set, files are served from disk (dev mode).
// Otherwise, embeddedFS is used (production mode with go:embed).
// If both are empty/nil, only /ui/health is registered.
func registerUIRoutes(mux *http.ServeMux, uiDir string, embeddedFS fs.FS) {
	mux.HandleFunc("GET /ui/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var fileServer http.Handler
	if uiDir != "" {
		fileServer = http.FileServer(http.Dir(uiDir))
	} else if embeddedFS != nil {
		sub, err := fs.Sub(embeddedFS, "dist")
		if err == nil {
			fileServer = http.FileServer(http.FS(sub))
		}
	}

	if fileServer != nil {
		mux.Handle("/ui/", http.StripPrefix("/ui/", fileServer))
	}
}
