// SPDX-License-Identifier: GPL-2.0-only
package daemon

import "github.com/mark3labs/mcp-go/mcp"

func newGetIdentityTokenTool() mcp.Tool {
	return mcp.NewTool("get_identity_token",
		mcp.WithDescription("Get the identity token for this session. Must be called first, then pass the token to register or identify."),
	)
}

func newRegisterTool() mcp.Tool {
	return mcp.NewTool("register",
		mcp.WithDescription("Create a new user and associate with identity token. Can only be called before identify."),
		mcp.WithString("token", mcp.Required(), mcp.Description("Identity token from get_identity_token")),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username to register")),
		mcp.WithString("password", mcp.Required(), mcp.Description("Password (reserved for future use)")),
	)
}

func newIdentifyTool() mcp.Tool {
	return mcp.NewTool("identify",
		mcp.WithDescription("Identify as an existing user and associate with identity token. Can only be called before register."),
		mcp.WithString("token", mcp.Required(), mcp.Description("Identity token from get_identity_token")),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username to identify as")),
		mcp.WithString("password", mcp.Required(), mcp.Description("Password (reserved for future use)")),
	)
}

func newUserListTool() mcp.Tool {
	return mcp.NewTool("user_list",
		mcp.WithDescription("List all registered users with their online/offline presence status"),
	)
}

func newChannelListTool() mcp.Tool {
	return mcp.NewTool("channel_list",
		mcp.WithDescription("List channels visible to you (public channels and channels you are a member of)"),
	)
}

func newChannelCreateTool() mcp.Tool {
	return mcp.NewTool("channel_create",
		mcp.WithDescription("Create a new channel. May be disabled by server configuration."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithBoolean("public", mcp.Required(), mcp.Description("Whether the channel is visible to all users")),
		mcp.WithArray("members", mcp.Description("Usernames of initial members"), mcp.WithStringItems()),
	)
}

func newChannelInviteTool() mcp.Tool {
	return mcp.NewTool("channel_invite",
		mcp.WithDescription("Add a user to a channel. You must be a participant of the channel."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username to invite")),
	)
}

func newChannelJoinTool() mcp.Tool {
	return mcp.NewTool("channel_join",
		mcp.WithDescription("Join a public channel. Private channels require an invite from an existing member."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
	)
}

func newSendMessageTool() mcp.Tool {
	return mcp.NewTool("send_message",
		mcp.WithDescription("Send a text message to a channel. You must be a participant of the channel."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Message text (UTF-8)")),
		mcp.WithArray("mentions", mcp.Description("Usernames to @mention in this message"), mcp.WithStringItems()),
		mcp.WithNumber("thread_id", mcp.Description("Message ID of the parent message to reply to (creates a thread)")),
	)
}

func newUnreadMessagesTool() mcp.Tool {
	return mcp.NewTool("unread_messages",
		mcp.WithDescription("Get unread messages across all your channels, or filtered by a specific channel"),
		mcp.WithString("channel", mcp.Description("Optional channel name to filter by")),
		mcp.WithBoolean("mentions_only", mcp.Description("If true, return only messages that @mention you")),
		mcp.WithNumber("thread_id", mcp.Description("If set, return only replies to this parent message ID")),
	)
}

func newUnreadCountsTool() mcp.Tool {
	return mcp.NewTool("unread_counts",
		mcp.WithDescription("Get unread message and mention counts per channel. Returns only channels with unreads."),
	)
}

func newMarkReadTool() mcp.Tool {
	return mcp.NewTool("mark_read",
		mcp.WithDescription("Mark a channel as read up to a specific message, or the latest message if not specified. Forward-only: cannot move cursor backwards."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithNumber("message_id", mcp.Description("Message ID to mark as read up to (default: latest)")),
	)
}

func newHistoryTool() mcp.Tool {
	return mcp.NewTool("history",
		mcp.WithDescription("Get message history for a channel. Returns the most recent messages in chronological order."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithNumber("before", mcp.Description("Return messages before this message ID (for pagination)")),
		mcp.WithNumber("after", mcp.Description("Return messages after this message ID")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of messages to return (default 50, max 100)")),
		mcp.WithNumber("thread_id", mcp.Description("If set, return only replies to this parent message ID")),
	)
}

func newDMListTool() mcp.Tool {
	return mcp.NewTool("dm_list",
		mcp.WithDescription("List your direct message conversations with other users"),
	)
}

func newDMOpenTool() mcp.Tool {
	return mcp.NewTool("dm_open",
		mcp.WithDescription("Open or create a direct message conversation with another user. Returns the DM channel name."),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username of the person to DM")),
	)
}

func newCapabilitiesTool() mcp.Tool {
	return mcp.NewTool("capabilities",
		mcp.WithDescription("Get your current permissions."),
	)
}

func newSetRoleTool() mcp.Tool {
	return mcp.NewTool("set_role",
		mcp.WithDescription("Set a user's role."),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username to update")),
		mcp.WithString("role", mcp.Required(), mcp.Description("Role to assign")),
	)
}

func newCreateRoleTool() mcp.Tool {
	return mcp.NewTool("create_role",
		mcp.WithDescription("Create a new custom role."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Role name")),
	)
}

func newDeleteRoleTool() mcp.Tool {
	return mcp.NewTool("delete_role",
		mcp.WithDescription("Delete a custom role. Built-in roles cannot be deleted."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Role name")),
	)
}

func newGrantPermissionTool() mcp.Tool {
	return mcp.NewTool("grant_permission",
		mcp.WithDescription("Grant a permission to a role."),
		mcp.WithString("role", mcp.Required(), mcp.Description("Role name")),
		mcp.WithString("permission", mcp.Required(), mcp.Description("Permission to grant")),
	)
}

func newRevokePermissionTool() mcp.Tool {
	return mcp.NewTool("revoke_permission",
		mcp.WithDescription("Revoke a permission from a role."),
		mcp.WithString("role", mcp.Required(), mcp.Description("Role name")),
		mcp.WithString("permission", mcp.Required(), mcp.Description("Permission to revoke")),
	)
}

func newListRolesTool() mcp.Tool {
	return mcp.NewTool("list_roles",
		mcp.WithDescription("List all roles and their permissions."),
	)
}
