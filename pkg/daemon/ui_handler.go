// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"io/fs"
	"net/http"
)

// uiHealthResponse is the JSON body the shell's service tracker expects
// from GET /ui/health. The tracker decodes this to discover the service's
// frontend manifest and WebSocket paths for proxying.
type uiHealthResponse struct {
	Status           string   `json:"status"`
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Route            string   `json:"route"`
	WSPaths          []string `json:"ws_paths"`
	NotificationPath string   `json:"notification_path,omitempty"`
}

// registerUIRoutes adds the /ui/health probe and static file serving.
// If uiDir is set, files are served from disk (dev mode).
// Otherwise, embeddedFS is used (production mode with go:embed).
// If both are empty/nil, only /ui/health is registered.
func registerUIRoutes(mux *http.ServeMux, uiDir string, embeddedFS fs.FS) {
	mux.HandleFunc("GET /ui/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(uiHealthResponse{
			Status:           "ok",
			Name:             "sharkfin",
			Label:            "Chat",
			Route:            "/chat",
			WSPaths:          []string{"/ws", "/presence"},
			NotificationPath: "/notifications/subscribe",
		})
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
