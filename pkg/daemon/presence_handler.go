// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"io"
	"net/http"
)

// PresenceHandler handles long-poll presence connections.
type PresenceHandler struct {
	sessions *SessionManager
}

// NewPresenceHandler creates a new presence handler.
func NewPresenceHandler(sessions *SessionManager) *PresenceHandler {
	return &PresenceHandler{sessions: sessions}
}

func (h *PresenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Token == "" {
		http.Error(w, "invalid request: token required", http.StatusBadRequest)
		return
	}

	done, err := h.sessions.AttachPresence(req.Token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Block until client disconnects or server closes the session
	select {
	case <-r.Context().Done():
	case <-done:
	}

	h.sessions.DisconnectPresence(req.Token)
}
