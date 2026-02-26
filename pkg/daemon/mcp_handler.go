// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/db"
	"github.com/Work-Fort/sharkfin/pkg/protocol"
)

// MCPHandler handles JSON-RPC 2.0 MCP requests.
type MCPHandler struct {
	sessions *SessionManager
	db       *db.DB
	hub      *Hub
}

// NewMCPHandler creates a new MCP handler.
func NewMCPHandler(sessions *SessionManager, database *db.DB, hub *Hub) *MCPHandler {
	return &MCPHandler{sessions: sessions, db: database, hub: hub}
}

func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONRPCError(w, nil, protocol.ParseError, "failed to read body")
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPCError(w, nil, protocol.ParseError, "invalid JSON")
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")

	switch req.Method {
	case "initialize":
		h.handleInitialize(w, &req)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		h.handleToolsList(w, &req)
	case "tools/call":
		h.handleToolsCall(w, &req, sessionID)
	default:
		writeJSONRPCError(w, req.ID, protocol.MethodNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (h *MCPHandler) handleInitialize(w http.ResponseWriter, req *protocol.Request) {
	sessionID := generateSessionID()
	w.Header().Set("Mcp-Session-Id", sessionID)

	result := map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"serverInfo": map[string]string{
			"name":    "sharkfin",
			"version": "0.1.0",
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
	}

	writeJSONRPCResult(w, req.ID, result)
}

func (h *MCPHandler) handleToolsList(w http.ResponseWriter, req *protocol.Request) {
	tools := map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "get_identity_token",
				"description": "Get the identity token for this session. Must be called first, then pass the token to register or identify.",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				"name":        "register",
				"description": "Create a new user and associate with identity token. Can only be called before identify.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"token":    map[string]string{"type": "string", "description": "Identity token from get_identity_token"},
						"username": map[string]string{"type": "string", "description": "Username to register"},
						"password": map[string]string{"type": "string", "description": "Password (reserved for future use)"},
					},
					"required": []string{"token", "username", "password"},
				},
			},
			{
				"name":        "identify",
				"description": "Identify as an existing user and associate with identity token. Can only be called before register.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"token":    map[string]string{"type": "string", "description": "Identity token from get_identity_token"},
						"username": map[string]string{"type": "string", "description": "Username to identify as"},
						"password": map[string]string{"type": "string", "description": "Password (reserved for future use)"},
					},
					"required": []string{"token", "username", "password"},
				},
			},
			{
				"name":        "user_list",
				"description": "List all registered users with their online/offline presence status",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				"name":        "channel_list",
				"description": "List channels visible to you (public channels and channels you are a member of)",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				"name":        "channel_create",
				"description": "Create a new channel. May be disabled by server configuration.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":    map[string]string{"type": "string", "description": "Channel name"},
						"public":  map[string]string{"type": "boolean", "description": "Whether the channel is visible to all users"},
						"members": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Usernames of initial members"},
					},
					"required": []string{"name", "public"},
				},
			},
			{
				"name":        "channel_invite",
				"description": "Add a user to a channel. You must be a participant of the channel.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"channel":  map[string]string{"type": "string", "description": "Channel name"},
						"username": map[string]string{"type": "string", "description": "Username to invite"},
					},
					"required": []string{"channel", "username"},
				},
			},
			{
				"name":        "send_message",
				"description": "Send a text message to a channel. You must be a participant of the channel.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"channel":   map[string]string{"type": "string", "description": "Channel name"},
						"message":   map[string]string{"type": "string", "description": "Message text (UTF-8)"},
						"mentions":  map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Usernames to @mention in this message"},
						"thread_id": map[string]interface{}{"type": "integer", "description": "Message ID of the parent message to reply to (creates a thread)"},
					},
					"required": []string{"channel", "message"},
				},
			},
			{
				"name":        "unread_messages",
				"description": "Get unread messages across all your channels, or filtered by a specific channel",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"channel":       map[string]string{"type": "string", "description": "Optional channel name to filter by"},
						"mentions_only": map[string]interface{}{"type": "boolean", "description": "If true, return only messages that @mention you"},
						"thread_id":     map[string]interface{}{"type": "integer", "description": "If set, return only replies to this parent message ID"},
					},
				},
			},
			{
				"name":        "history",
				"description": "Get message history for a channel. Returns the most recent messages in chronological order.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"channel":   map[string]string{"type": "string", "description": "Channel name"},
						"before":    map[string]interface{}{"type": "integer", "description": "Return messages before this message ID (for pagination)"},
						"after":     map[string]interface{}{"type": "integer", "description": "Return messages after this message ID"},
						"limit":     map[string]interface{}{"type": "integer", "description": "Maximum number of messages to return (default 50, max 100)"},
						"thread_id": map[string]interface{}{"type": "integer", "description": "If set, return only replies to this parent message ID"},
					},
					"required": []string{"channel"},
				},
			},
		},
	}

	writeJSONRPCResult(w, req.ID, tools)
}

func (h *MCPHandler) handleToolsCall(w http.ResponseWriter, req *protocol.Request, sessionID string) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid tool call params")
		return
	}

	// Tools that don't require identification
	switch params.Name {
	case "register":
		h.handleRegister(w, req, params.Arguments, sessionID)
		return
	case "identify":
		h.handleIdentifyTool(w, req, params.Arguments, sessionID)
		return
	}

	// All other tools require an identified session
	session, err := h.sessions.GetSession(sessionID)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, "not identified: call register or identify first")
		return
	}

	switch params.Name {
	case "user_list":
		h.handleUserList(w, req, session)
	case "channel_list":
		h.handleChannelList(w, req, session)
	case "channel_create":
		h.handleChannelCreate(w, req, params.Arguments, session)
	case "channel_invite":
		h.handleChannelInvite(w, req, params.Arguments, session)
	case "send_message":
		h.handleSendMessage(w, req, params.Arguments, session)
	case "unread_messages":
		h.handleUnreadMessages(w, req, params.Arguments, session)
	case "history":
		h.handleHistory(w, req, params.Arguments, session)
	default:
		writeJSONRPCError(w, req.ID, protocol.MethodNotFound, fmt.Sprintf("unknown tool: %s", params.Name))
	}
}

func (h *MCPHandler) handleRegister(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, sessionID string) {
	var a struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	// Check if this session is already identified
	if sessionID != "" {
		if _, err := h.sessions.GetSession(sessionID); err == nil {
			writeJSONRPCError(w, req.ID, -32001, "session already identified")
			return
		}
	}

	mcpSessionID, err := h.sessions.Register(a.Token, a.Username, a.Password)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, err.Error())
		return
	}

	w.Header().Set("Mcp-Session-Id", mcpSessionID)
	writeToolResult(w, req.ID, fmt.Sprintf("registered as %s", a.Username))
}

func (h *MCPHandler) handleIdentifyTool(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, sessionID string) {
	var a struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	// Check if this session is already identified
	if sessionID != "" {
		if _, err := h.sessions.GetSession(sessionID); err == nil {
			writeJSONRPCError(w, req.ID, -32001, "session already identified")
			return
		}
	}

	mcpSessionID, err := h.sessions.Identify(a.Token, a.Username, a.Password)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, err.Error())
		return
	}

	w.Header().Set("Mcp-Session-Id", mcpSessionID)
	writeToolResult(w, req.ID, fmt.Sprintf("identified as %s", a.Username))
}

func (h *MCPHandler) handleUserList(w http.ResponseWriter, req *protocol.Request, session *MCPSession) {
	users, err := h.db.ListUsers()
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
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

	data, _ := json.Marshal(list)
	writeToolResult(w, req.ID, string(data))
}

func (h *MCPHandler) handleChannelList(w http.ResponseWriter, req *protocol.Request, session *MCPSession) {
	user, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	channels, err := h.db.ListChannelsForUser(user.ID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
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

	data, _ := json.Marshal(list)
	writeToolResult(w, req.ID, string(data))
}

func (h *MCPHandler) handleChannelCreate(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, session *MCPSession) {
	val, err := h.db.GetSetting("allow_channel_creation")
	if err == nil && val == "false" {
		writeJSONRPCError(w, req.ID, -32001, "channel creation is disabled")
		return
	}

	var a struct {
		Name    string   `json:"name"`
		Public  bool     `json:"public"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	// Resolve creator's user ID
	creator, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	// Resolve member user IDs (always include creator)
	memberIDs := []int64{creator.ID}
	for _, username := range a.Members {
		u, err := h.db.GetUserByUsername(username)
		if err != nil {
			writeJSONRPCError(w, req.ID, -32001, fmt.Sprintf("user not found: %s", username))
			return
		}
		if u.ID != creator.ID {
			memberIDs = append(memberIDs, u.ID)
		}
	}

	chID, err := h.db.CreateChannel(a.Name, a.Public, memberIDs)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	writeToolResult(w, req.ID, fmt.Sprintf("created channel %s (id: %d)", a.Name, chID))
}

func (h *MCPHandler) handleChannelInvite(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, session *MCPSession) {
	var a struct {
		Channel  string `json:"channel"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	caller, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	ch, err := h.getChannelByName(a.Channel)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, err.Error())
		return
	}

	// Check caller is a participant
	isMember, err := h.db.IsChannelMember(ch.ID, caller.ID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}
	if !isMember {
		writeJSONRPCError(w, req.ID, -32001, "you are not a participant of this channel")
		return
	}

	invitee, err := h.db.GetUserByUsername(a.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, fmt.Sprintf("user not found: %s", a.Username))
		return
	}

	if err := h.db.AddChannelMember(ch.ID, invitee.ID); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	writeToolResult(w, req.ID, fmt.Sprintf("invited %s to %s", a.Username, a.Channel))
}

func (h *MCPHandler) handleSendMessage(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, session *MCPSession) {
	var a struct {
		Channel  string   `json:"channel"`
		Message  string   `json:"message"`
		Mentions []string `json:"mentions"`
		ThreadID *int64   `json:"thread_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	sender, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	ch, err := h.getChannelByName(a.Channel)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, sender.ID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}
	if !isMember {
		writeJSONRPCError(w, req.ID, -32001, "you are not a participant of this channel")
		return
	}

	var mentionUserIDs []int64
	var mentionUsernames []string
	for _, username := range a.Mentions {
		u, err := h.db.GetUserByUsername(username)
		if err != nil {
			writeJSONRPCError(w, req.ID, -32001, fmt.Sprintf("mentioned user not found: %s", username))
			return
		}
		mentionUserIDs = append(mentionUserIDs, u.ID)
		mentionUsernames = append(mentionUsernames, u.Username)
	}

	msgID, err := h.db.SendMessage(ch.ID, sender.ID, a.Message, a.ThreadID, mentionUserIDs)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	writeToolResult(w, req.ID, fmt.Sprintf("message sent (id: %d)", msgID))

	// Broadcast to WebSocket clients
	msg := db.Message{
		ID:        msgID,
		ChannelID: ch.ID,
		UserID:    sender.ID,
		Body:      a.Message,
		CreatedAt: time.Now(),
		Username:  session.Username,
	}
	h.hub.BroadcastMessage(ch.ID, a.Channel, msg, mentionUsernames, a.ThreadID, h.db)
}

func (h *MCPHandler) handleUnreadMessages(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, session *MCPSession) {
	var a struct {
		Channel      string `json:"channel"`
		MentionsOnly bool   `json:"mentions_only"`
		ThreadID     *int64 `json:"thread_id"`
	}
	if args != nil {
		json.Unmarshal(args, &a)
	}

	user, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	var channelID *int64
	if a.Channel != "" {
		ch, err := h.getChannelByName(a.Channel)
		if err != nil {
			writeJSONRPCError(w, req.ID, -32001, err.Error())
			return
		}
		channelID = &ch.ID
	}

	messages, err := h.db.GetUnreadMessages(user.ID, channelID, a.MentionsOnly, a.ThreadID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
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
			if ch, err := h.db.GetChannelByID(m.ChannelID); err == nil {
				chName = ch.Name
			}
			channelNames[m.ChannelID] = chName
		}
		list = append(list, msgInfo{
			Channel:  chName,
			From:     m.Username,
			Body:     m.Body,
			SentAt:   m.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ThreadID: m.ThreadID,
			Mentions: m.Mentions,
		})
	}

	data, _ := json.Marshal(list)
	writeToolResult(w, req.ID, string(data))
}

func (h *MCPHandler) handleHistory(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, session *MCPSession) {
	var a struct {
		Channel  string `json:"channel"`
		Before   *int64 `json:"before"`
		After    *int64 `json:"after"`
		Limit    int    `json:"limit"`
		ThreadID *int64 `json:"thread_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	user, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	ch, err := h.getChannelByName(a.Channel)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, user.ID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}
	if !isMember {
		writeJSONRPCError(w, req.ID, -32001, "you are not a participant of this channel")
		return
	}

	if a.Limit <= 0 {
		a.Limit = 50
	}

	messages, err := h.db.GetMessages(ch.ID, a.Before, a.After, a.Limit, a.ThreadID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	type msgInfo struct {
		Channel  string   `json:"channel"`
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
			Channel:  a.Channel,
			ID:       m.ID,
			From:     m.Username,
			Body:     m.Body,
			SentAt:   m.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ThreadID: m.ThreadID,
			Mentions: m.Mentions,
		})
	}

	data, _ := json.Marshal(list)
	writeToolResult(w, req.ID, string(data))
}

func (h *MCPHandler) getChannelByName(name string) (*db.Channel, error) {
	return h.db.GetChannelByName(name)
}

// --- Helpers ---

func writeJSONRPCResult(w http.ResponseWriter, id *protocol.RequestID, result interface{}) {
	data, _ := json.Marshal(result)
	resp := protocol.NewResponse(id, data)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeJSONRPCError(w http.ResponseWriter, id *protocol.RequestID, code int, message string) {
	resp := protocol.NewErrorResponse(id, code, message)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeToolResult(w http.ResponseWriter, id *protocol.RequestID, text string) {
	result := map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
	writeJSONRPCResult(w, id, result)
}
