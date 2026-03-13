// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	"github.com/Work-Fort/sharkfin/pkg/domain"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSHandler handles WebSocket connections for non-MCP clients.
type WSHandler struct {
	store       domain.Store
	hub         *Hub
	presence    *PresenceHandler
	pongTimeout time.Duration
	version     string
}

// NewWSHandler creates a new WebSocket handler.
func NewWSHandler(store domain.Store, hub *Hub, presence *PresenceHandler, pongTimeout time.Duration, version string) *WSHandler {
	return &WSHandler{store: store, hub: hub, presence: presence, pongTimeout: pongTimeout, version: version}
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Auto-provision identity.
	role := identity.Type
	if role == "" {
		role = "user"
	}
	if err := h.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role); err != nil {
		http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
		return
	}

	localIdentity, err := h.store.GetIdentityByID(identity.ID)
	if err != nil {
		http.Error(w, "identity lookup failed", http.StatusInternalServerError)
		return
	}

	notificationsOnly := r.URL.Query().Get("notifications_only") == "true"

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	username := localIdentity.Username
	identityID := localIdentity.ID
	pingInterval := h.pongTimeout / 2

	client := &WSClient{username: username, identityID: identityID, send: make(chan []byte, 256), hub: h.hub}
	h.hub.Register(client)
	h.hub.SetState(username, "idle")
	h.hub.BroadcastPresence(username, true, "idle")
	log.Info("ws: connect", "user", username, "notifications_only", notificationsOnly, "clients", h.hub.ClientCount())

	defer func() {
		h.hub.Unregister(client)
		h.hub.ClearState(username)
		h.hub.BroadcastPresence(username, false, "")
		log.Info("ws: disconnect", "user", username, "clients", h.hub.ClientCount())
	}()

	// Set up keepalive
	conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
		return nil
	})

	// Write pump goroutine — sends outbound messages and pings
	sendCh := client.send
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				conn.SetWriteDeadline(time.Now().Add(pingInterval))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(pingInterval))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// Read pump — reads client requests
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req wsRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			sendError(sendCh, "", "invalid JSON")
			continue
		}

		t0 := time.Now()
		switch req.Type {
		case "ping":
			sendPong(sendCh, req.Ref)
		case "version":
			sendReply(sendCh, req.Ref, true, map[string]string{"version": h.version})

		// Allowed in notifications_only mode (no permission check needed)
		case "capabilities":
			perms, err := h.store.GetUserPermissions(username)
			if err != nil {
				sendError(sendCh, req.Ref, err.Error())
				break
			}
			sendReply(sendCh, req.Ref, true, map[string]interface{}{"permissions": perms})
		case "set_state":
			var d struct {
				State string `json:"state"`
			}
			json.Unmarshal(req.D, &d)
			if d.State != "active" && d.State != "idle" {
				sendError(sendCh, req.Ref, "state must be 'active' or 'idle'")
				break
			}
			h.hub.SetState(username, d.State)
			h.hub.BroadcastPresence(username, true, d.State)
			sendReply(sendCh, req.Ref, true, nil)

		// All remaining actions are blocked in notifications_only mode
		case "user_list":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "user_list") {
				h.handleWSUserList(sendCh, req.Ref)
			}
		case "channel_list":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "channel_list") {
				h.handleWSChannelList(sendCh, req.Ref, identityID)
			}
		case "channel_create":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "create_channel") {
				h.handleWSChannelCreate(sendCh, req.Ref, req.D, username, identityID)
			}
		case "channel_invite":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "invite_channel") {
				h.handleWSChannelInvite(sendCh, req.Ref, req.D, identityID)
			}
		case "channel_join":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "join_channel") {
				h.handleWSChannelJoin(sendCh, req.Ref, req.D, identityID)
			}
		case "send_message":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "send_message") {
				h.handleWSSendMessage(sendCh, req.Ref, req.D, username, identityID)
			}
		case "history":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "history") {
				h.handleWSHistory(sendCh, req.Ref, req.D, identityID)
			}
		case "unread_messages":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "unread_messages") {
				h.handleWSUnreadMessages(sendCh, req.Ref, req.D, identityID)
			}
		case "unread_counts":
			if h.checkPermission(sendCh, req.Ref, username, "unread_counts") {
				h.handleWSUnreadCounts(sendCh, req.Ref, identityID)
			}
		case "dm_list":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "dm_list") {
				h.handleWSDMList(sendCh, req.Ref, identityID)
			}
		case "dm_open":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "dm_open") {
				h.handleWSDMOpen(sendCh, req.Ref, req.D, username, identityID)
			}
		case "mark_read":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "mark_read") {
				h.handleWSMarkRead(sendCh, req.Ref, req.D, identityID)
			}
		case "set_setting":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "manage_roles") {
				h.handleWSSetSetting(sendCh, req.Ref, req.D)
			}
		case "get_settings":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else if h.checkPermission(sendCh, req.Ref, username, "manage_roles") {
				h.handleWSGetSettings(sendCh, req.Ref)
			}
		case "mention_group_create":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else {
				h.handleWSMentionGroupCreate(sendCh, req.Ref, req.D, identityID)
			}
		case "mention_group_delete":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else {
				h.handleWSMentionGroupDelete(sendCh, req.Ref, req.D, username)
			}
		case "mention_group_get":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else {
				h.handleWSMentionGroupGet(sendCh, req.Ref, req.D)
			}
		case "mention_group_list":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else {
				h.handleWSMentionGroupList(sendCh, req.Ref)
			}
		case "mention_group_add_member":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else {
				h.handleWSMentionGroupAddMember(sendCh, req.Ref, req.D, username)
			}
		case "mention_group_remove_member":
			if notificationsOnly {
				sendError(sendCh, req.Ref, "notification-only connection")
			} else {
				h.handleWSMentionGroupRemoveMember(sendCh, req.Ref, req.D, username)
			}
		default:
			sendError(sendCh, req.Ref, fmt.Sprintf("unknown type: %s", req.Type))
		}
		if elapsed := time.Since(t0); elapsed > 50*time.Millisecond {
			log.Warn("ws: slow handler", "type", req.Type, "user", username, "elapsed", elapsed)
		}
	}
}

// checkPermission verifies the user has the given permission, sending an error if not.
func (h *WSHandler) checkPermission(sendCh chan<- []byte, ref, username, permission string) bool {
	ok, err := h.store.HasPermission(username, permission)
	if err != nil || !ok {
		sendError(sendCh, ref, fmt.Sprintf("permission denied: %s", permission))
		return false
	}
	return true
}

// --- Request handlers ---

func (h *WSHandler) handleWSUserList(sendCh chan<- []byte, ref string) {
	identities, err := h.store.ListIdentities()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	type userInfo struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
		Type     string `json:"type"`
		State    string `json:"state,omitempty"`
	}
	var list []userInfo
	for _, i := range identities {
		online := h.presence.IsOnline(i.Username)
		info := userInfo{
			Username: i.Username,
			Online:   online,
			Type:     i.Type,
		}
		if online {
			info.State = h.hub.GetState(i.Username)
		}
		list = append(list, info)
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"users": list})
}

func (h *WSHandler) handleWSChannelList(sendCh chan<- []byte, ref string, identityID string) {
	t0 := time.Now()
	channels, err := h.store.ListAllChannelsWithMembership(identityID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	dbElapsed := time.Since(t0)
	type channelInfo struct {
		Name   string `json:"name"`
		Public bool   `json:"public"`
		Member bool   `json:"member"`
	}
	var list []channelInfo
	for _, ch := range channels {
		list = append(list, channelInfo{Name: ch.Name, Public: ch.Public, Member: ch.Member})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"channels": list})
	log.Debug("ws: channel_list", "identity_id", identityID, "count", len(list), "db", dbElapsed, "total", time.Since(t0))
}

func (h *WSHandler) handleWSChannelCreate(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string, identityID string) {
	var d struct {
		Name    string   `json:"name"`
		Public  bool     `json:"public"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	memberIDs := []string{identityID}
	for _, m := range d.Members {
		identity, err := h.store.GetIdentityByUsername(m)
		if err != nil {
			sendError(sendCh, ref, fmt.Sprintf("user not found: %s", m))
			return
		}
		if identity.ID != identityID {
			memberIDs = append(memberIDs, identity.ID)
		}
	}

	_, err := h.store.CreateChannel(d.Name, d.Public, memberIDs, "channel")
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]string{"name": d.Name})
}

func (h *WSHandler) handleWSChannelInvite(sendCh chan<- []byte, ref string, rawD json.RawMessage, callerID string) {
	var d struct {
		Channel  string `json:"channel"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.store.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.store.IsChannelMember(ch.ID, callerID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	invitee, err := h.store.GetIdentityByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}

	if err := h.store.AddChannelMember(ch.ID, invitee.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSChannelJoin(sendCh chan<- []byte, ref string, rawD json.RawMessage, identityID string) {
	var d struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.store.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.store.IsChannelMember(ch.ID, identityID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if isMember {
		sendError(sendCh, ref, "already a member")
		return
	}

	if err := h.store.AddChannelMember(ch.ID, identityID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSSendMessage(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string, identityID string) {
	var d struct {
		Channel  string `json:"channel"`
		Body     string `json:"body"`
		ThreadID *int64 `json:"thread_id"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.store.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.store.IsChannelMember(ch.ID, identityID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	mentionIDs, mentionUsernames := resolveMentions(h.store, d.Body)

	msgID, err := h.store.SendMessage(ch.ID, identityID, d.Body, d.ThreadID, mentionIDs)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	sendReply(sendCh, ref, true, map[string]interface{}{"id": msgID})

	// Broadcast to other WS clients
	msg := domain.Message{
		ID:         msgID,
		ChannelID:  ch.ID,
		IdentityID: identityID,
		Body:       d.Body,
		CreatedAt:  time.Now(),
		From:       username,
	}
	h.hub.BroadcastMessage(ch.ID, ch.Name, ch.Type, msg, mentionUsernames, d.ThreadID, h.store)
}

func (h *WSHandler) handleWSHistory(sendCh chan<- []byte, ref string, rawD json.RawMessage, identityID string) {
	var d struct {
		Channel  string `json:"channel"`
		Before   *int64 `json:"before"`
		After    *int64 `json:"after"`
		Limit    int    `json:"limit"`
		ThreadID *int64 `json:"thread_id"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.store.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	if d.Limit <= 0 {
		d.Limit = 50
	}

	messages, err := h.store.GetMessages(ch.ID, d.Before, d.After, d.Limit, d.ThreadID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	type msgInfo struct {
		ID       int64    `json:"id"`
		From     string   `json:"from"`
		Body     string   `json:"body"`
		SentAt   string   `json:"sent_at"`
		ThreadID *int64   `json:"thread_id,omitempty"`
		Mentions []string `json:"mentions,omitempty"`
	}
	var list []msgInfo
	for _, m := range messages {
		list = append(list, msgInfo{
			ID:       m.ID,
			From:     m.From,
			Body:     m.Body,
			SentAt:   m.CreatedAt.UTC().Format(time.RFC3339),
			ThreadID: m.ThreadID,
			Mentions: m.Mentions,
		})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"channel": d.Channel, "messages": list})
}

func (h *WSHandler) handleWSUnreadMessages(sendCh chan<- []byte, ref string, rawD json.RawMessage, identityID string) {
	var d struct {
		Channel      string `json:"channel"`
		MentionsOnly bool   `json:"mentions_only"`
		ThreadID     *int64 `json:"thread_id"`
	}
	if rawD != nil {
		json.Unmarshal(rawD, &d)
	}

	var channelID *int64
	if d.Channel != "" {
		ch, err := h.store.GetChannelByName(d.Channel)
		if err != nil {
			sendError(sendCh, ref, err.Error())
			return
		}
		channelID = &ch.ID
	}

	messages, err := h.store.GetUnreadMessages(identityID, channelID, d.MentionsOnly, d.ThreadID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	channelNames := make(map[int64]string)
	type msgInfo struct {
		Channel  string   `json:"channel"`
		From     string   `json:"from"`
		Body     string   `json:"body"`
		SentAt   string   `json:"sent_at"`
		ThreadID *int64   `json:"thread_id,omitempty"`
		Mentions []string `json:"mentions,omitempty"`
	}
	var list []msgInfo
	for _, m := range messages {
		chName, ok := channelNames[m.ChannelID]
		if !ok {
			if ch, err := h.store.GetChannelByID(m.ChannelID); err == nil {
				chName = ch.Name
			}
			channelNames[m.ChannelID] = chName
		}
		list = append(list, msgInfo{
			Channel:  chName,
			From:     m.From,
			Body:     m.Body,
			SentAt:   m.CreatedAt.UTC().Format(time.RFC3339),
			ThreadID: m.ThreadID,
			Mentions: m.Mentions,
		})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"messages": list})
}

func (h *WSHandler) handleWSUnreadCounts(sendCh chan<- []byte, ref string, identityID string) {
	counts, err := h.store.GetUnreadCounts(identityID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	type countInfo struct {
		Channel      string `json:"channel"`
		Type         string `json:"type"`
		UnreadCount  int    `json:"unread_count"`
		MentionCount int    `json:"mention_count"`
	}
	var list []countInfo
	for _, c := range counts {
		list = append(list, countInfo{
			Channel:      c.Channel,
			Type:         c.Type,
			UnreadCount:  c.UnreadCount,
			MentionCount: c.MentionCount,
		})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"counts": list})
}

func (h *WSHandler) handleWSDMList(sendCh chan<- []byte, ref string, identityID string) {
	dms, err := h.store.ListAllDMs()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	type dmInfo struct {
		Channel      string   `json:"channel"`
		Participants []string `json:"participants"`
	}
	var list []dmInfo
	for _, dm := range dms {
		list = append(list, dmInfo{Channel: dm.ChannelName, Participants: []string{dm.User1Username, dm.User2Username}})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"dms": list})
}

func (h *WSHandler) handleWSDMOpen(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string, identityID string) {
	var d struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	if d.Username == "" {
		sendError(sendCh, ref, "username is required")
		return
	}
	if d.Username == username {
		sendError(sendCh, ref, "cannot open DM with yourself")
		return
	}

	other, err := h.store.GetIdentityByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}

	dmName, created, err := h.store.OpenDM(identityID, other.ID, d.Username)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{
		"channel":     dmName,
		"participant": d.Username,
		"created":     created,
	})
}

func (h *WSHandler) handleWSMarkRead(sendCh chan<- []byte, ref string, rawD json.RawMessage, identityID string) {
	var d struct {
		Channel   string `json:"channel"`
		MessageID *int64 `json:"message_id"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	if d.Channel == "" {
		sendError(sendCh, ref, "channel is required")
		return
	}

	ch, err := h.store.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.store.IsChannelMember(ch.ID, identityID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	if err := h.store.MarkRead(identityID, ch.ID, d.MessageID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSSetSetting(sendCh chan<- []byte, ref string, rawD json.RawMessage) {
	var d struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	if err := h.store.SetSetting(d.Key, d.Value); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSGetSettings(sendCh chan<- []byte, ref string) {
	settings, err := h.store.ListSettings()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"settings": settings})
}

// --- Mention group handlers ---

func (h *WSHandler) handleWSMentionGroupCreate(sendCh chan<- []byte, ref string, rawD json.RawMessage, identityID string) {
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	if !slugRe.MatchString(d.Slug) {
		sendError(sendCh, ref, "invalid slug: must match [a-zA-Z0-9_-]+")
		return
	}
	if _, err := h.store.GetIdentityByUsername(d.Slug); err == nil {
		sendError(sendCh, ref, fmt.Sprintf("slug conflicts with existing username: %s", d.Slug))
		return
	}
	id, err := h.store.CreateMentionGroup(d.Slug, identityID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"id": id, "slug": d.Slug})
}

func (h *WSHandler) handleWSMentionGroupDelete(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string) {
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if g.CreatedBy != username {
		sendError(sendCh, ref, "only the group creator can delete it")
		return
	}
	if err := h.store.DeleteMentionGroup(g.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSMentionGroupGet(sendCh chan<- []byte, ref string, rawD json.RawMessage) {
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, g)
}

func (h *WSHandler) handleWSMentionGroupList(sendCh chan<- []byte, ref string) {
	groups, err := h.store.ListMentionGroups()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"groups": groups})
}

func (h *WSHandler) handleWSMentionGroupAddMember(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string) {
	var d struct {
		Slug     string `json:"slug"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if g.CreatedBy != username {
		sendError(sendCh, ref, "only the group creator can manage members")
		return
	}
	identity, err := h.store.GetIdentityByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}
	if err := h.store.AddMentionGroupMember(g.ID, identity.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSMentionGroupRemoveMember(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string) {
	var d struct {
		Slug     string `json:"slug"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if g.CreatedBy != username {
		sendError(sendCh, ref, "only the group creator can manage members")
		return
	}
	identity, err := h.store.GetIdentityByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}
	if err := h.store.RemoveMentionGroupMember(g.ID, identity.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

// --- Wire types and helpers ---

type wsRequest struct {
	Type string          `json:"type"`
	D    json.RawMessage `json:"d"`
	Ref  string          `json:"ref"`
}

func sendReply(sendCh chan<- []byte, ref string, ok bool, d interface{}) {
	env := wsEnvelope{Type: "reply", Ref: ref, OK: &ok, D: d}
	data, _ := json.Marshal(env)
	select {
	case sendCh <- data:
	default:
	}
}

func sendError(sendCh chan<- []byte, ref string, message string) {
	ok := false
	env := wsEnvelope{Type: "error", Ref: ref, OK: &ok, D: map[string]string{"message": message}}
	data, _ := json.Marshal(env)
	select {
	case sendCh <- data:
	default:
	}
}

func sendPong(sendCh chan<- []byte, ref string) {
	ok := true
	env := wsEnvelope{Type: "pong", Ref: ref, OK: &ok}
	data, _ := json.Marshal(env)
	select {
	case sendCh <- data:
	default:
	}
}

func writeWSJSON(conn *websocket.Conn, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}
