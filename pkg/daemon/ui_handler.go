// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import "net/http"

func registerUIRoutes(mux *http.ServeMux, uiDir string) {
	mux.HandleFunc("GET /ui/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if uiDir != "" {
		mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir(uiDir))))
	}
}
