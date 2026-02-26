// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

// WSHandler handles WebSocket connections for non-MCP clients.
type WSHandler struct {
	sessions    *SessionManager
	db          *db.DB
	hub         *Hub
	pongTimeout time.Duration
}

// NewWSHandler creates a new WebSocket handler.
func NewWSHandler(sessions *SessionManager, database *db.DB, hub *Hub, pongTimeout time.Duration) *WSHandler {
	return &WSHandler{
		sessions:    sessions,
		db:          database,
		hub:         hub,
		pongTimeout: pongTimeout,
	}
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	pingInterval := h.pongTimeout / 2

	// Send hello
	hello := wsEnvelope{
		Type: "hello",
		D: map[string]interface{}{
			"heartbeat_interval": int(pingInterval.Seconds()),
		},
	}
	if err := writeWSJSON(conn, hello); err != nil {
		return
	}

	// Connection state
	var client *WSClient
	var identified bool
	var username string
	var userID int64
	token := h.sessions.CreateIdentityToken()
	done, _ := h.sessions.AttachPresence(token)
	defer func() {
		if identified {
			h.hub.Unregister(client)
			h.hub.BroadcastPresence(username, false)
		}
		h.sessions.DisconnectPresence(token)
	}()

	// Set up keepalive
	conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
		return nil
	})

	// Write pump goroutine — sends outbound messages and pings
	sendCh := make(chan []byte, 256)
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
			case <-done:
				return
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

		// Before identification: only allow identify/register/ping
		if !identified {
			switch req.Type {
			case "identify":
				var d struct {
					Username string `json:"username"`
				}
				json.Unmarshal(req.D, &d)
				if d.Username == "" {
					sendReply(sendCh, req.Ref, false, map[string]string{"message": "username required"})
					continue
				}
				_, err := h.sessions.Identify(token, d.Username, "")
				if err != nil {
					sendReply(sendCh, req.Ref, false, map[string]string{"message": err.Error()})
					continue
				}
				u, err := h.db.GetUserByUsername(d.Username)
				if err != nil {
					sendReply(sendCh, req.Ref, false, map[string]string{"message": err.Error()})
					continue
				}
				identified = true
				username = d.Username
				userID = u.ID
				client = &WSClient{username: username, userID: userID, send: sendCh, hub: h.hub}
				h.hub.Register(client)
				h.hub.BroadcastPresence(username, true)
				sendReply(sendCh, req.Ref, true, nil)

			case "register":
				var d struct {
					Username string `json:"username"`
				}
				json.Unmarshal(req.D, &d)
				if d.Username == "" {
					sendReply(sendCh, req.Ref, false, map[string]string{"message": "username required"})
					continue
				}
				_, err := h.sessions.Register(token, d.Username, "")
				if err != nil {
					sendReply(sendCh, req.Ref, false, map[string]string{"message": err.Error()})
					continue
				}
				u, err := h.db.GetUserByUsername(d.Username)
				if err != nil {
					sendReply(sendCh, req.Ref, false, map[string]string{"message": err.Error()})
					continue
				}
				identified = true
				username = d.Username
				userID = u.ID
				client = &WSClient{username: username, userID: userID, send: sendCh, hub: h.hub}
				h.hub.Register(client)
				h.hub.BroadcastPresence(username, true)
				sendReply(sendCh, req.Ref, true, nil)

			case "ping":
				sendPong(sendCh, req.Ref)

			default:
				sendError(sendCh, req.Ref, "not identified: send identify or register first")
			}
			continue
		}

		// After identification: dispatch all request types
		switch req.Type {
		case "identify", "register":
			sendError(sendCh, req.Ref, "already identified")
		case "ping":
			sendPong(sendCh, req.Ref)
		case "user_list":
			h.handleWSUserList(sendCh, req.Ref)
		case "channel_list":
			h.handleWSChannelList(sendCh, req.Ref, userID)
		case "channel_create":
			h.handleWSChannelCreate(sendCh, req.Ref, req.D, username, userID)
		case "channel_invite":
			h.handleWSChannelInvite(sendCh, req.Ref, req.D, userID)
		case "send_message":
			h.handleWSSendMessage(sendCh, req.Ref, req.D, username, userID)
		case "history":
			h.handleWSHistory(sendCh, req.Ref, req.D, userID)
		case "set_setting":
			h.handleWSSetSetting(sendCh, req.Ref, req.D)
		case "get_settings":
			h.handleWSGetSettings(sendCh, req.Ref)
		default:
			sendError(sendCh, req.Ref, fmt.Sprintf("unknown type: %s", req.Type))
		}
	}
}

// --- Request handlers ---

func (h *WSHandler) handleWSUserList(sendCh chan<- []byte, ref string) {
	users, err := h.db.ListUsers()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	type userInfo struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
	}
	var list []userInfo
	for _, u := range users {
		list = append(list, userInfo{
			Username: u.Username,
			Online:   h.sessions.IsUserOnline(u.Username),
		})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"users": list})
}

func (h *WSHandler) handleWSChannelList(sendCh chan<- []byte, ref string, userID int64) {
	channels, err := h.db.ListChannelsForUser(userID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	type channelInfo struct {
		Name   string `json:"name"`
		Public bool   `json:"public"`
	}
	var list []channelInfo
	for _, ch := range channels {
		list = append(list, channelInfo{Name: ch.Name, Public: ch.Public})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"channels": list})
}

func (h *WSHandler) handleWSChannelCreate(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string, userID int64) {
	// Check setting
	val, err := h.db.GetSetting("allow_channel_creation")
	if err == nil && val == "false" {
		sendError(sendCh, ref, "channel creation is disabled")
		return
	}

	var d struct {
		Name    string   `json:"name"`
		Public  bool     `json:"public"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	memberIDs := []int64{userID}
	for _, m := range d.Members {
		u, err := h.db.GetUserByUsername(m)
		if err != nil {
			sendError(sendCh, ref, fmt.Sprintf("user not found: %s", m))
			return
		}
		if u.ID != userID {
			memberIDs = append(memberIDs, u.ID)
		}
	}

	_, err = h.db.CreateChannel(d.Name, d.Public, memberIDs)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]string{"name": d.Name})
}

func (h *WSHandler) handleWSChannelInvite(sendCh chan<- []byte, ref string, rawD json.RawMessage, callerID int64) {
	var d struct {
		Channel  string `json:"channel"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.db.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, callerID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	invitee, err := h.db.GetUserByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}

	if err := h.db.AddChannelMember(ch.ID, invitee.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSSendMessage(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string, userID int64) {
	var d struct {
		Channel string `json:"channel"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.db.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, userID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	msgID, err := h.db.SendMessage(ch.ID, userID, d.Body)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	sendReply(sendCh, ref, true, map[string]interface{}{"id": msgID})

	// Broadcast to other WS clients
	msg := db.Message{
		ID:        msgID,
		ChannelID: ch.ID,
		UserID:    userID,
		Body:      d.Body,
		CreatedAt: time.Now(),
		Username:  username,
	}
	h.hub.BroadcastMessage(ch.ID, ch.Name, msg, h.db)
}

func (h *WSHandler) handleWSHistory(sendCh chan<- []byte, ref string, rawD json.RawMessage, userID int64) {
	var d struct {
		Channel string `json:"channel"`
		Before  *int64 `json:"before"`
		After   *int64 `json:"after"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}

	ch, err := h.db.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, userID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	if d.Limit <= 0 {
		d.Limit = 50
	}

	messages, err := h.db.GetMessages(ch.ID, d.Before, d.After, d.Limit)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	type msgInfo struct {
		ID     int64  `json:"id"`
		From   string `json:"from"`
		Body   string `json:"body"`
		SentAt string `json:"sent_at"`
	}
	var list []msgInfo
	for _, m := range messages {
		list = append(list, msgInfo{
			ID:     m.ID,
			From:   m.Username,
			Body:   m.Body,
			SentAt: m.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"messages": list})
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
	if err := h.db.SetSetting(d.Key, d.Value); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSGetSettings(sendCh chan<- []byte, ref string) {
	settings, err := h.db.ListSettings()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"settings": settings})
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
