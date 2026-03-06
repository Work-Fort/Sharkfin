// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

type contextKey int

const usernameKey contextKey = iota

// toolPermissions maps tool names to the permission required to invoke them.
// Tools not in this map (e.g. get_identity_token, register, identify,
// capabilities, set_state) pass through without permission checks.
var toolPermissions = map[string]string{
	"user_list":         "user_list",
	"channel_list":      "channel_list",
	"channel_create":    "create_channel",
	"channel_invite":    "invite_channel",
	"channel_join":      "join_channel",
	"send_message":      "send_message",
	"unread_messages":   "unread_messages",
	"unread_counts":     "unread_counts",
	"mark_read":         "mark_read",
	"history":           "history",
	"dm_list":           "dm_list",
	"dm_open":           "dm_open",
	"set_role":          "manage_roles",
	"create_role":       "manage_roles",
	"delete_role":       "manage_roles",
	"grant_permission":  "manage_roles",
	"revoke_permission": "manage_roles",
	"list_roles":        "manage_roles",
}

// SharkfinMCP wraps an mcp-go MCPServer with Sharkfin's business logic.
type SharkfinMCP struct {
	mcpServer *server.MCPServer
	sessions  *SessionManager
	store     domain.Store
	hub       *Hub

	mu             sync.RWMutex
	mcpGoUsernames map[string]string // mcp-go session ID → username
}

// NewSharkfinMCP creates the MCP server and registers all tools.
func NewSharkfinMCP(sm *SessionManager, store domain.Store, hub *Hub) *SharkfinMCP {
	s := &SharkfinMCP{
		sessions:       sm,
		store:          store,
		hub:            hub,
		mcpGoUsernames: make(map[string]string),
	}

	hooks := &server.Hooks{}
	hooks.AddOnUnregisterSession(func(ctx context.Context, sess server.ClientSession) {
		s.mu.Lock()
		delete(s.mcpGoUsernames, sess.SessionID())
		s.mu.Unlock()
	})

	s.mcpServer = server.NewMCPServer("sharkfin", "0.1.0",
		server.WithHooks(hooks),
		server.WithToolHandlerMiddleware(s.authMiddleware),
	)

	s.mcpServer.AddTools(
		server.ServerTool{Tool: newGetIdentityTokenTool(), Handler: s.handleGetIdentityToken},
		server.ServerTool{Tool: newRegisterTool(), Handler: s.handleRegister},
		server.ServerTool{Tool: newIdentifyTool(), Handler: s.handleIdentify},
		server.ServerTool{Tool: newUserListTool(), Handler: s.handleUserList},
		server.ServerTool{Tool: newChannelListTool(), Handler: s.handleChannelList},
		server.ServerTool{Tool: newChannelCreateTool(), Handler: s.handleChannelCreate},
		server.ServerTool{Tool: newChannelInviteTool(), Handler: s.handleChannelInvite},
		server.ServerTool{Tool: newChannelJoinTool(), Handler: s.handleChannelJoin},
		server.ServerTool{Tool: newSendMessageTool(), Handler: s.handleSendMessage},
		server.ServerTool{Tool: newUnreadMessagesTool(), Handler: s.handleUnreadMessages},
		server.ServerTool{Tool: newUnreadCountsTool(), Handler: s.handleUnreadCounts},
		server.ServerTool{Tool: newMarkReadTool(), Handler: s.handleMarkRead},
		server.ServerTool{Tool: newHistoryTool(), Handler: s.handleHistory},
		server.ServerTool{Tool: newDMListTool(), Handler: s.handleDMList},
		server.ServerTool{Tool: newDMOpenTool(), Handler: s.handleDMOpen},
		server.ServerTool{Tool: newCapabilitiesTool(), Handler: s.handleCapabilities},
		server.ServerTool{Tool: newSetRoleTool(), Handler: s.handleSetRole},
		server.ServerTool{Tool: newCreateRoleTool(), Handler: s.handleCreateRole},
		server.ServerTool{Tool: newDeleteRoleTool(), Handler: s.handleDeleteRole},
		server.ServerTool{Tool: newGrantPermissionTool(), Handler: s.handleGrantPermission},
		server.ServerTool{Tool: newRevokePermissionTool(), Handler: s.handleRevokePermission},
		server.ServerTool{Tool: newListRolesTool(), Handler: s.handleListRoles},
		server.ServerTool{Tool: newSetStateTool(), Handler: s.handleSetState},
	)

	return s
}

// Server returns the underlying mcp-go MCPServer.
func (s *SharkfinMCP) Server() *server.MCPServer {
	return s.mcpServer
}

// setUsername associates a mcp-go session ID with a username.
func (s *SharkfinMCP) setUsername(mcpGoSessionID, username string) {
	s.mu.Lock()
	s.mcpGoUsernames[mcpGoSessionID] = username
	s.mu.Unlock()
}

// getUsername looks up the username for a mcp-go session ID.
func (s *SharkfinMCP) getUsername(mcpGoSessionID string) (string, bool) {
	s.mu.RLock()
	u, ok := s.mcpGoUsernames[mcpGoSessionID]
	s.mu.RUnlock()
	return u, ok
}

// authMiddleware enforces identification for all tools except public ones.
func (s *SharkfinMCP) authMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		switch req.Params.Name {
		case "get_identity_token", "register", "identify":
			return next(ctx, req)
		}

		sess := server.ClientSessionFromContext(ctx)
		if sess == nil {
			return mcp.NewToolResultError("not identified: call register or identify first"), nil
		}
		username, ok := s.getUsername(sess.SessionID())
		if !ok {
			return mcp.NewToolResultError("not identified: call register or identify first"), nil
		}

		if perm, ok := toolPermissions[req.Params.Name]; ok {
			hasPerm, err := s.store.HasPermission(username, perm)
			if err != nil || !hasPerm {
				return mcp.NewToolResultError(fmt.Sprintf("permission denied: %s", perm)), nil
			}
		}

		t0 := time.Now()
		ctx = context.WithValue(ctx, usernameKey, username)
		result, err := next(ctx, req)
		if elapsed := time.Since(t0); elapsed > 50*time.Millisecond {
			log.Warn("mcp: slow tool", "tool", req.Params.Name, "user", username, "elapsed", elapsed)
		}
		return result, err
	}
}

func usernameFromCtx(ctx context.Context) string {
	return ctx.Value(usernameKey).(string)
}

// --- Tool handlers ---

func (s *SharkfinMCP) handleGetIdentityToken(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// The bridge intercepts this call and returns the token directly.
	// This handler exists only so the tool appears in tools/list.
	return mcp.NewToolResultError("get_identity_token must be called through the MCP bridge"), nil
}

func (s *SharkfinMCP) handleRegister(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	token := req.GetString("token", "")
	username := req.GetString("username", "")
	password := req.GetString("password", "")

	sess := server.ClientSessionFromContext(ctx)
	if sess == nil {
		return mcp.NewToolResultError("no session"), nil
	}

	// Check if this mcp-go session is already identified.
	if _, ok := s.getUsername(sess.SessionID()); ok {
		return mcp.NewToolResultError("session already identified"), nil
	}

	if _, err := s.sessions.Register(token, username, password); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.setUsername(sess.SessionID(), username)
	s.store.SetUserType(username, "agent")
	return mcp.NewToolResultText(fmt.Sprintf("registered as %s", username)), nil
}

func (s *SharkfinMCP) handleIdentify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	token := req.GetString("token", "")
	username := req.GetString("username", "")
	password := req.GetString("password", "")

	sess := server.ClientSessionFromContext(ctx)
	if sess == nil {
		return mcp.NewToolResultError("no session"), nil
	}

	// Check if this mcp-go session is already identified.
	if _, ok := s.getUsername(sess.SessionID()); ok {
		return mcp.NewToolResultError("session already identified"), nil
	}

	if _, err := s.sessions.Identify(token, username, password); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.setUsername(sess.SessionID(), username)
	return mcp.NewToolResultText(fmt.Sprintf("identified as %s", username)), nil
}

func (s *SharkfinMCP) handleUserList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	users, err := s.store.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	type userInfo struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
		Type     string `json:"type"`
		State    string `json:"state,omitempty"`
	}
	var list []userInfo
	for _, u := range users {
		online := s.sessions.IsUserOnline(u.Username)
		info := userInfo{
			Username: u.Username,
			Online:   online,
			Type:     u.Type,
		}
		if online {
			info.State = s.hub.GetState(u.Username)
		}
		list = append(list, info)
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleChannelList(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	channels, err := s.store.ListChannelsForUser(user.ID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
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
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleChannelCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	public := req.GetBool("public", false)
	members := req.GetStringSlice("members", nil)

	creator, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	memberIDs := []int64{creator.ID}
	for _, username := range members {
		u, err := s.store.GetUserByUsername(username)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", username)), nil
		}
		if u.ID != creator.ID {
			memberIDs = append(memberIDs, u.ID)
		}
	}

	chID, err := s.store.CreateChannel(name, public, memberIDs, "channel")
	if err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("created channel %s (id: %d)", name, chID)), nil
}

func (s *SharkfinMCP) handleChannelInvite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channel := req.GetString("channel", "")
	username := req.GetString("username", "")

	caller, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, caller.ID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return mcp.NewToolResultError("you are not a participant of this channel"), nil
	}

	invitee, err := s.store.GetUserByUsername(username)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", username)), nil
	}

	if err := s.store.AddChannelMember(ch.ID, invitee.ID); err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("invited %s to %s", username, channel)), nil
}

func (s *SharkfinMCP) handleChannelJoin(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channel := req.GetString("channel", "")

	caller, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	if !ch.Public {
		return mcp.NewToolResultError("channel is private, requires an invite"), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, caller.ID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if isMember {
		return mcp.NewToolResultError("already a member"), nil
	}

	if err := s.store.AddChannelMember(ch.ID, caller.ID); err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("joined %s", channel)), nil
}

func (s *SharkfinMCP) handleSendMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channel := req.GetString("channel", "")
	message := req.GetString("message", "")
	mentionsList := req.GetStringSlice("mentions", nil)
	threadID := optionalInt64(req, "thread_id")

	sender, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, sender.ID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return mcp.NewToolResultError("you are not a participant of this channel"), nil
	}

	mentionUserIDs, mentionUsernames := resolveMentions(s.store, message, mentionsList)

	msgID, err := s.store.SendMessage(ch.ID, sender.ID, message, threadID, mentionUserIDs)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	// Broadcast to WebSocket clients.
	msg := domain.Message{
		ID:        msgID,
		ChannelID: ch.ID,
		UserID:    sender.ID,
		Body:      message,
		CreatedAt: time.Now(),
		From:      usernameFromCtx(ctx),
	}
	s.hub.BroadcastMessage(ch.ID, channel, ch.Type, msg, mentionUsernames, threadID, s.store)

	return mcp.NewToolResultText(fmt.Sprintf("message sent (id: %d)", msgID)), nil
}

func (s *SharkfinMCP) handleUnreadMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channelName := req.GetString("channel", "")
	mentionsOnly := req.GetBool("mentions_only", false)
	threadID := optionalInt64(req, "thread_id")

	user, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	var channelID *int64
	if channelName != "" {
		ch, err := s.store.GetChannelByName(channelName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channelName)), nil
		}
		channelID = &ch.ID
	}

	messages, err := s.store.GetUnreadMessages(user.ID, channelID, mentionsOnly, threadID)
	if err != nil {
		return nil, fmt.Errorf("get unreads: %w", err)
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
			if ch, err := s.store.GetChannelByID(m.ChannelID); err == nil {
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

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleUnreadCounts(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	counts, err := s.store.GetUnreadCounts(user.ID)
	if err != nil {
		return nil, fmt.Errorf("get counts: %w", err)
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

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleMarkRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channel := req.GetString("channel", "")
	messageID := optionalInt64(req, "message_id")

	user, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, user.ID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return mcp.NewToolResultError("you are not a participant of this channel"), nil
	}

	if err := s.store.MarkRead(user.ID, ch.ID, messageID); err != nil {
		return nil, fmt.Errorf("mark read: %w", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("marked %s as read", channel)), nil
}

func (s *SharkfinMCP) handleHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channel := req.GetString("channel", "")
	before := optionalInt64(req, "before")
	after := optionalInt64(req, "after")
	limit := req.GetInt("limit", 50)
	threadID := optionalInt64(req, "thread_id")

	user, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, user.ID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return mcp.NewToolResultError("you are not a participant of this channel"), nil
	}

	if limit <= 0 {
		limit = 50
	}

	messages, err := s.store.GetMessages(ch.ID, before, after, limit, threadID)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
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
			Channel:  channel,
			ID:       m.ID,
			From:     m.From,
			Body:     m.Body,
			SentAt:   m.CreatedAt.UTC().Format(time.RFC3339),
			ThreadID: m.ThreadID,
			Mentions: m.Mentions,
		})
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleDMList(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	dms, err := s.store.ListDMsForUser(user.ID)
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}

	type dmInfo struct {
		Channel     string `json:"channel"`
		Participant string `json:"participant"`
	}
	var list []dmInfo
	for _, dm := range dms {
		list = append(list, dmInfo{Channel: dm.ChannelName, Participant: dm.OtherUsername})
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleDMOpen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	targetUsername := req.GetString("username", "")
	callerUsername := usernameFromCtx(ctx)

	if targetUsername == callerUsername {
		return mcp.NewToolResultError("cannot open DM with yourself"), nil
	}

	caller, err := s.store.GetUserByUsername(callerUsername)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	other, err := s.store.GetUserByUsername(targetUsername)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", targetUsername)), nil
	}

	dmName, created, err := s.store.OpenDM(caller.ID, other.ID, targetUsername)
	if err != nil {
		return nil, fmt.Errorf("open dm: %w", err)
	}

	result := map[string]interface{}{
		"channel":     dmName,
		"participant": targetUsername,
		"created":     created,
	}
	data, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleCapabilities(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	username := usernameFromCtx(ctx)
	perms, err := s.store.GetUserPermissions(username)
	if err != nil {
		return nil, fmt.Errorf("get permissions: %w", err)
	}
	data, _ := json.Marshal(perms)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleSetRole(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	username := req.GetString("username", "")
	role := req.GetString("role", "")

	if err := s.store.SetUserRole(username, role); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.broadcastCapabilities(role)
	return mcp.NewToolResultText(fmt.Sprintf("set %s role to %s", username, role)), nil
}

func (s *SharkfinMCP) handleCreateRole(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	if err := s.store.CreateRole(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("created role %s", name)), nil
}

func (s *SharkfinMCP) handleDeleteRole(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	if err := s.store.DeleteRole(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("deleted role %s", name)), nil
}

func (s *SharkfinMCP) handleGrantPermission(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	role := req.GetString("role", "")
	permission := req.GetString("permission", "")

	if err := s.store.GrantPermission(role, permission); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.broadcastCapabilities(role)
	return mcp.NewToolResultText(fmt.Sprintf("granted %s to %s", permission, role)), nil
}

func (s *SharkfinMCP) handleRevokePermission(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	role := req.GetString("role", "")
	permission := req.GetString("permission", "")

	if err := s.store.RevokePermission(role, permission); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.broadcastCapabilities(role)
	return mcp.NewToolResultText(fmt.Sprintf("revoked %s from %s", permission, role)), nil
}

func (s *SharkfinMCP) handleListRoles(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	roles, err := s.store.ListRoles()
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	type roleInfo struct {
		Name        string   `json:"name"`
		BuiltIn     bool     `json:"built_in"`
		Permissions []string `json:"permissions"`
	}
	var list []roleInfo
	for _, r := range roles {
		perms, err := s.store.GetRolePermissions(r.Name)
		if err != nil {
			return nil, fmt.Errorf("get role permissions: %w", err)
		}
		list = append(list, roleInfo{
			Name:        r.Name,
			BuiltIn:     r.BuiltIn,
			Permissions: perms,
		})
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleSetState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	state := req.GetString("state", "")
	if state != "active" && state != "idle" {
		return mcp.NewToolResultError("state must be 'active' or 'idle'"), nil
	}
	username := usernameFromCtx(ctx)
	s.hub.SetState(username, state)
	s.hub.BroadcastPresence(username, true, state)
	return mcp.NewToolResultText(fmt.Sprintf("state set to %s", state)), nil
}

// broadcastCapabilities sends a capabilities event to all WS clients with the given role.
func (s *SharkfinMCP) broadcastCapabilities(role string) {
	perms, err := s.store.GetRolePermissions(role)
	if err != nil {
		return
	}
	data, _ := json.Marshal(map[string]interface{}{"permissions": perms})
	event := wsEnvelope{Type: "capabilities", D: json.RawMessage(data)}
	eventData, _ := json.Marshal(event)

	s.hub.BroadcastToRole(role, eventData, s.store)
}

// optionalInt64 extracts an optional integer argument, returning nil if absent.
func optionalInt64(req mcp.CallToolRequest, key string) *int64 {
	args := req.GetArguments()
	if args == nil {
		return nil
	}
	val, ok := args[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case float64:
		i := int64(v)
		return &i
	case int:
		i := int64(v)
		return &i
	}
	return nil
}
