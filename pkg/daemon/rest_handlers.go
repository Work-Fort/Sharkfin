// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/charmbracelet/log"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// restIdentity auto-provisions and returns the calling identity.
// Returns nil and writes 401/500 on failure.
func restIdentity(w http.ResponseWriter, r *http.Request, store domain.Store) *domain.Identity {
	passportIdent, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	role := passportIdent.Type
	if role == "" {
		role = "user"
	}
	localIdentity, err := store.UpsertIdentity(passportIdent.ID, passportIdent.Username, passportIdent.DisplayName, passportIdent.Type, role)
	if err != nil {
		log.Error("rest: identity provisioning", "err", err)
		http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
		return nil
	}
	return localIdentity
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// RESTHandler holds all REST endpoint handlers.
type RESTHandler struct {
	store domain.Store
	hub   *Hub
	bus   domain.EventBus
}

func NewRESTHandler(store domain.Store, hub *Hub, bus domain.EventBus) *RESTHandler {
	return &RESTHandler{store: store, hub: hub, bus: bus}
}

// --- POST /api/v1/auth/register ---

func (h *RESTHandler) handleRegisterIdentity(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       identity.ID,
		"username": identity.Username,
		"type":     identity.Type,
		"role":     identity.Role,
	})
}

// --- GET /api/v1/channels ---

func (h *RESTHandler) handleListChannels(w http.ResponseWriter, r *http.Request) {
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	channels, err := h.store.ListAllChannelsWithMembership(identity.ID)
	if err != nil {
		log.Error("rest: list channels", "err", err)
		http.Error(w, "list channels failed", http.StatusInternalServerError)
		return
	}
	type channelResp struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Public bool   `json:"public"`
		Member bool   `json:"member"`
	}
	var out []channelResp
	for _, ch := range channels {
		out = append(out, channelResp{ID: ch.ID, Name: ch.Name, Public: ch.Public, Member: ch.Member})
	}
	if out == nil {
		out = []channelResp{}
	}
	writeJSON(w, http.StatusOK, out)
}

// --- POST /api/v1/channels ---

func (h *RESTHandler) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Public bool   `json:"public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	channelID, err := h.store.CreateChannel(req.Name, req.Public, []string{identity.ID}, "channel")
	if err != nil {
		log.Error("rest: create channel", "err", err)
		http.Error(w, "create channel failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     channelID,
		"name":   req.Name,
		"public": req.Public,
	})
}

// --- POST /api/v1/channels/{channel}/join ---

func (h *RESTHandler) handleJoinChannel(w http.ResponseWriter, r *http.Request) {
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	channelName := r.PathValue("channel")
	ch, err := h.store.GetChannelByName(channelName)
	if err != nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if err := h.store.AddChannelMember(ch.ID, identity.ID); err != nil {
		log.Error("rest: join channel", "err", err)
		http.Error(w, "join channel failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// --- POST /api/v1/channels/{channel}/messages ---

func (h *RESTHandler) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	channelName := r.PathValue("channel")
	ch, err := h.store.GetChannelByName(channelName)
	if err != nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	var req struct {
		Body     string         `json:"body"`
		Metadata map[string]any `json:"metadata"`
		ThreadID *int64         `json:"thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}

	var metadataStr *string
	if req.Metadata != nil {
		b, _ := json.Marshal(req.Metadata)
		s := string(b)
		metadataStr = &s
	}

	sentAt := time.Now()
	msgID, err := h.store.SendMessage(ch.ID, identity.ID, req.Body, req.ThreadID, nil, metadataStr)
	if err != nil {
		log.Error("rest: send message", "err", err)
		http.Error(w, "send message failed", http.StatusInternalServerError)
		return
	}

	// Publish to event bus so WebhookSubscriber and hub broadcast pick it up,
	// exactly as the WS handler does.
	if h.bus != nil {
		h.bus.Publish(domain.Event{
			Type: domain.EventMessageNew,
			Payload: domain.MessageEvent{
				ChannelName: channelName,
				ChannelType: ch.Type,
				From:        identity.Username,
				Body:        req.Body,
				MessageID:   msgID,
				SentAt:      sentAt,
				ThreadID:    req.ThreadID,
				Metadata:    metadataStr,
			},
		})
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      msgID,
		"body":    req.Body,
		"metadata": req.Metadata,
		"sent_at": sentAt.UTC().Format(time.RFC3339),
	})
}

// --- GET /api/v1/channels/{channel}/messages ---

func (h *RESTHandler) handleListMessages(w http.ResponseWriter, r *http.Request) {
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	channelName := r.PathValue("channel")
	ch, err := h.store.GetChannelByName(channelName)
	if err != nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	q := r.URL.Query()
	var before, after *int64
	var limit int = 50
	if v := q.Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			before = &n
		}
	}
	if v := q.Get("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			after = &n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	msgs, err := h.store.GetMessages(ch.ID, before, after, limit, nil)
	if err != nil {
		log.Error("rest: list messages", "err", err)
		http.Error(w, "list messages failed", http.StatusInternalServerError)
		return
	}

	type msgResp struct {
		ID       int64   `json:"id"`
		From     string  `json:"from"`
		Body     string  `json:"body"`
		Metadata *string `json:"metadata,omitempty"`
		ThreadID *int64  `json:"thread_id,omitempty"`
		SentAt   string  `json:"sent_at"`
	}
	out := make([]msgResp, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, msgResp{
			ID:       m.ID,
			From:     m.From,
			Body:     m.Body,
			Metadata: m.Metadata,
			ThreadID: m.ThreadID,
			SentAt:   m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- POST /api/v1/webhooks ---

func (h *RESTHandler) handleRegisterWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	webhookID, err := h.store.RegisterWebhook(identity.ID, req.URL)
	if err != nil {
		log.Error("rest: register webhook", "err", err)
		http.Error(w, "register webhook failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     webhookID,
		"url":    req.URL,
		"active": true,
	})
}

// --- GET /api/v1/webhooks ---

func (h *RESTHandler) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	hooks, err := h.store.GetActiveWebhooksForIdentity(identity.ID)
	if err != nil {
		log.Error("rest: list webhooks", "err", err)
		http.Error(w, "list webhooks failed", http.StatusInternalServerError)
		return
	}
	type hookResp struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Active bool   `json:"active"`
	}
	out := make([]hookResp, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, hookResp{ID: h.ID, URL: h.URL, Active: h.Active})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- DELETE /api/v1/webhooks/{id} ---

func (h *RESTHandler) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	identity := restIdentity(w, r, h.store)
	if identity == nil {
		return
	}
	webhookID := r.PathValue("id")
	if webhookID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.store.UnregisterWebhook(identity.ID, webhookID); err != nil {
		log.Error("rest: delete webhook", "err", err)
		http.Error(w, "delete webhook failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
