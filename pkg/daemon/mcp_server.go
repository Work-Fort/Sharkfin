// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/charmbracelet/log"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	"github.com/Work-Fort/sharkfin/pkg/domain"
)

var slugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type contextKey int

const usernameKey contextKey = iota

// toolPermissions maps tool names to the permission required to invoke them.
// Tools not in this map (e.g. capabilities, set_state) pass through
// without permission checks.
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
	store     domain.Store
	hub       *Hub
	presence  *PresenceHandler
}

// NewSharkfinMCP creates the MCP server and registers all tools.
func NewSharkfinMCP(store domain.Store, hub *Hub, presence *PresenceHandler, version string) *SharkfinMCP {
	s := &SharkfinMCP{
		store:    store,
		hub:      hub,
		presence: presence,
	}

	s.mcpServer = server.NewMCPServer("sharkfin", version,
		server.WithToolHandlerMiddleware(s.authMiddleware),
	)

	s.mcpServer.AddTools(
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
		server.ServerTool{Tool: newWaitForMessagesTool(), Handler: s.handleWaitForMessages},
		server.ServerTool{Tool: newMentionGroupCreateTool(), Handler: s.handleMentionGroupCreate},
		server.ServerTool{Tool: newMentionGroupDeleteTool(), Handler: s.handleMentionGroupDelete},
		server.ServerTool{Tool: newMentionGroupGetTool(), Handler: s.handleMentionGroupGet},
		server.ServerTool{Tool: newMentionGroupListTool(), Handler: s.handleMentionGroupList},
		server.ServerTool{Tool: newMentionGroupAddMemberTool(), Handler: s.handleMentionGroupAddMember},
		server.ServerTool{Tool: newMentionGroupRemoveMemberTool(), Handler: s.handleMentionGroupRemoveMember},
		server.ServerTool{Tool: newRegisterWebhookTool(), Handler: s.handleRegisterWebhook},
		server.ServerTool{Tool: newUnregisterWebhookTool(), Handler: s.handleUnregisterWebhook},
		server.ServerTool{Tool: newListWebhooksTool(), Handler: s.handleListWebhooks},
	)

	return s
}

// Server returns the underlying mcp-go MCPServer.
func (s *SharkfinMCP) Server() *server.MCPServer {
	return s.mcpServer
}

// authMiddleware uses Passport context to authenticate and auto-provision identities.
func (s *SharkfinMCP) authMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		identity, ok := auth.IdentityFromContext(ctx)
		if !ok {
			return mcp.NewToolResultError("unauthorized: no valid token"), nil
		}

		// Auto-provision
		role := identity.Type
		if role == "" {
			role = "user"
		}
		if _, err := s.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role); err != nil {
			log.Warn("identity provisioning failed", "err", err, "username", identity.Username)
		}

		username := identity.Username

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

func (s *SharkfinMCP) handleUserList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	identities, err := s.store.ListIdentities()
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}

	type userInfo struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
		Type     string `json:"type"`
		State    string `json:"state,omitempty"`
	}
	var list []userInfo
	for _, id := range identities {
		online := s.presence.IsOnline(id.Username)
		info := userInfo{
			Username: id.Username,
			Online:   online,
			Type:     id.Type,
		}
		if online {
			info.State = s.hub.GetState(id.Username)
		}
		list = append(list, info)
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleChannelList(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	channels, err := s.store.ListChannelsForUser(identity.ID)
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

	creator, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	memberIDs := []string{creator.ID}
	for _, username := range members {
		u, err := s.store.GetIdentityByUsername(username)
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

	caller, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
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

	invitee, err := s.store.GetIdentityByUsername(username)
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

	caller, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
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
	threadID := optionalInt64(req, "thread_id")

	sender, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
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

	mentionIdentityIDs, mentionUsernames := resolveMentions(s.store, message)
	metadata := optionalString(req, "metadata")

	msgID, err := s.store.SendMessage(ch.ID, sender.ID, message, threadID, mentionIdentityIDs, metadata)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	// Broadcast to WebSocket clients.
	msg := domain.Message{
		ID:         msgID,
		ChannelID:  ch.ID,
		IdentityID: sender.ID,
		Body:       message,
		CreatedAt:  time.Now(),
		From:       usernameFromCtx(ctx),
		Metadata:   metadata,
	}
	s.hub.BroadcastMessage(ch.ID, channel, ch.Type, msg, mentionUsernames, threadID, s.store)

	return mcp.NewToolResultText(fmt.Sprintf("message sent (id: %d)", msgID)), nil
}

func (s *SharkfinMCP) handleUnreadMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channelName := req.GetString("channel", "")
	mentionsOnly := req.GetBool("mentions_only", false)
	threadID := optionalInt64(req, "thread_id")

	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	var channelID *int64
	if channelName != "" {
		ch, err := s.store.GetChannelByName(channelName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channelName)), nil
		}
		channelID = &ch.ID
	}

	messages, err := s.store.GetUnreadMessages(identity.ID, channelID, mentionsOnly, threadID)
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
		Metadata *string  `json:"metadata,omitempty"`
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
			Metadata: m.Metadata,
		})
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleUnreadCounts(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	counts, err := s.store.GetUnreadCounts(identity.ID)
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

	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, identity.ID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return mcp.NewToolResultError("you are not a participant of this channel"), nil
	}

	if err := s.store.MarkRead(identity.ID, ch.ID, messageID); err != nil {
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

	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	ch, err := s.store.GetChannelByName(channel)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("channel not found: %s", channel)), nil
	}

	isMember, err := s.store.IsChannelMember(ch.ID, identity.ID)
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
		Metadata *string  `json:"metadata,omitempty"`
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
			Metadata: m.Metadata,
		})
	}

	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleDMList(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	dms, err := s.store.ListDMsForUser(identity.ID)
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

	caller, err := s.store.GetIdentityByUsername(callerUsername)
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	other, err := s.store.GetIdentityByUsername(targetUsername)
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

func (s *SharkfinMCP) handleWaitForMessages(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// The bridge intercepts this call and handles it client-side.
	// This handler exists only so the tool appears in tools/list.
	return mcp.NewToolResultError("wait_for_messages is only available via mcp-bridge"), nil
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

// optionalString extracts an optional string argument, returning nil if absent or empty.
// Empty string treated as absent; metadata must be valid JSON if provided.
func optionalString(req mcp.CallToolRequest, key string) *string {
	v := req.GetString(key, "")
	if v == "" {
		return nil
	}
	return &v
}

// --- Mention group handlers ---

func (s *SharkfinMCP) handleMentionGroupCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	if !slugRe.MatchString(slug) {
		return mcp.NewToolResultError("invalid slug: must match [a-zA-Z0-9_-]+"), nil
	}
	// Reject if slug collides with an existing username.
	if _, err := s.store.GetIdentityByUsername(slug); err == nil {
		return mcp.NewToolResultError(fmt.Sprintf("slug conflicts with existing username: %s", slug)), nil
	}
	sender, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}
	id, err := s.store.CreateMentionGroup(slug, sender.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("created mention group @%s (id: %d)", slug, id)), nil
}

func (s *SharkfinMCP) handleMentionGroupDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if g.CreatedBy != usernameFromCtx(ctx) {
		return mcp.NewToolResultError("only the group creator can delete it"), nil
	}
	if err := s.store.DeleteMentionGroup(g.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("deleted mention group @%s", slug)), nil
}

func (s *SharkfinMCP) handleMentionGroupGet(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _ := json.Marshal(g)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleMentionGroupList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groups, err := s.store.ListMentionGroups()
	if err != nil {
		return nil, fmt.Errorf("list mention groups: %w", err)
	}
	data, _ := json.Marshal(groups)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleMentionGroupAddMember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	username := req.GetString("username", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if g.CreatedBy != usernameFromCtx(ctx) {
		return mcp.NewToolResultError("only the group creator can manage members"), nil
	}
	identity, err := s.store.GetIdentityByUsername(username)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", username)), nil
	}
	if err := s.store.AddMentionGroupMember(g.ID, identity.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("added %s to @%s", username, slug)), nil
}

func (s *SharkfinMCP) handleMentionGroupRemoveMember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	username := req.GetString("username", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if g.CreatedBy != usernameFromCtx(ctx) {
		return mcp.NewToolResultError("only the group creator can manage members"), nil
	}
	identity, err := s.store.GetIdentityByUsername(username)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", username)), nil
	}
	if err := s.store.RemoveMentionGroupMember(g.ID, identity.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("removed %s from @%s", username, slug)), nil
}

// --- Webhook handlers ---

func (s *SharkfinMCP) handleRegisterWebhook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := req.GetString("url", "")
	if url == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	if _, err := s.store.RegisterWebhook(identity.ID, url); err != nil {
		return nil, fmt.Errorf("register webhook: %w", err)
	}

	return mcp.NewToolResultText("webhook registered"), nil
}

func (s *SharkfinMCP) handleUnregisterWebhook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	webhookID := req.GetString("webhook_id", "")
	if webhookID == "" {
		return mcp.NewToolResultError("webhook_id is required"), nil
	}

	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	if err := s.store.UnregisterWebhook(identity.ID, webhookID); err != nil {
		return nil, fmt.Errorf("unregister webhook: %w", err)
	}

	return mcp.NewToolResultText("webhook unregistered"), nil
}

func (s *SharkfinMCP) handleListWebhooks(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}

	hooks, err := s.store.GetActiveWebhooksForIdentity(identity.ID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}

	type hookInfo struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	list := make([]hookInfo, 0, len(hooks))
	for _, h := range hooks {
		list = append(list, hookInfo{ID: h.ID, URL: h.URL})
	}
	data, _ := json.Marshal(list)
	return mcp.NewToolResultText(string(data)), nil
}
