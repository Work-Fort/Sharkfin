# Bot / Service Identity Requirements

## Purpose

Enable WorkFort services (Flow, Combine, Hive, etc.) to register as bot
identities in Sharkfin, receive channel messages via webhooks, and send
structured notifications. This is a prerequisite for Flow's Sharkfin bot
adapter (Phase 2b) and the broader "bots as service avatars" pattern across
the platform.

## Background

Sharkfin's identity model already supports a `type` field with values `"user"`,
`"agent"`, and `"service"` (domain `Identity` struct). The messaging
infrastructure (channels, cursors, send/receive, history) is fully functional.
However, several gaps prevent services from operating as first-class
participants.

## Requirements

### 1. Bot Role Seed

**What:** Add a built-in `bot` role to the RBAC migration with appropriate
permissions.

**Why:** `UpsertIdentity` with `type = "service"` succeeds today, but the
identity lands on the default `user` role. Services need a distinct role with
permissions scoped to bot operations (send messages, read channels, join
channels) without admin capabilities.

**Details:**
- New migration: `INSERT INTO roles (name, built_in) VALUES ('bot', 1)`
- Assign permissions: `channel.join`, `channel.read`, `message.send`,
  `message.read`, `channel.list`
- `UpsertIdentity` should auto-assign the `bot` role when `type = "service"`

### 2. Message Metadata

**What:** Add an optional `metadata` JSON column to messages for structured
app-to-app data (event types, payloads, state changes).

**Why:** Flow's bot needs to attach structured context to messages (e.g.
workflow state changes, task references) without overloading the message body.
Other services will have their own metadata needs. A JSON sidecar is more
flexible and extensible than a fixed enum — no migration needed when new use
cases arise. Visual distinction between bot and human messages is handled by
the sender's `identity.type`, not a message field.

**Details:**
- Add `metadata TEXT` column to `messages` table (nullable, JSON string)
- Update `SendMessage` store method and `send_message` MCP tool to accept
  optional `metadata` parameter (JSON object)
- Update `GetMessages`, `GetUnreadMessages` to return `metadata` in response
- Metadata convention: `{"event_type": "resource_verb", "event_payload": {...}}`
  (e.g. `{"event_type": "task_transitioned", "event_payload": {"id": "TK-42", "to": "review"}}`)
- Metadata is opaque to Sharkfin — it stores and returns it without interpretation
- The UI can use metadata presence + `identity.type` to determine rendering

### 3. Webhook Recipient Scope

**What:** Extend webhook notifications to include bot/service identities that
are members of a channel, not just @mentioned users and DM participants.

**Why:** Currently `computeRecipients` only notifies on @mentions and DMs. A
bot subscribed to a channel never receives webhook notifications unless
explicitly @mentioned. Bots need to receive all messages in channels they've
joined.

**Details:**
- In `computeRecipients`, after computing mention/DM recipients, also include
  all channel members whose identity `type = "service"`
- This ensures bots receive every message in their channels without requiring
  @mentions
- Regular users and agents are NOT included (they use the existing
  mention/DM/notification model)

### 4. Per-Identity Webhook Registration

**What:** Allow each identity to register its own webhook callback URL,
replacing the current single global `webhook_url` setting.

**Why:** Multiple services (Flow, Combine, etc.) each need their own webhook
endpoint. A single global URL cannot route to multiple consumers.

**Details:**
- New table: `identity_webhooks` with fields:
  - `id TEXT PRIMARY KEY`
  - `identity_id TEXT NOT NULL REFERENCES identities(id)`
  - `url TEXT NOT NULL` (callback URL)
  - `secret TEXT NOT NULL DEFAULT ''` (for HMAC signature verification)
  - `active INTEGER NOT NULL DEFAULT 1`
  - `created_at DATETIME NOT NULL DEFAULT (datetime('now'))`
- New MCP tool: `register_webhook` — registers a callback URL for the
  calling identity
- New MCP tool: `unregister_webhook` — removes the registration
- Webhook dispatch: when a message is sent, for each recipient with a
  registered webhook, POST the payload to their URL
- Payload format (extends existing `WebhookPayload`):
  ```json
  {
    "event": "message.new",
    "channel_id": "...",
    "channel_name": "...",
    "from": "...",
    "from_type": "agent",
    "message_id": "...",
    "body": "...",
    "metadata": null,
    "sent_at": "..."
  }
  ```
- `message_id` in the payload is the anchor for bot reply threading —
  bots pass it as `thread_id` in `send_message` to reply in context
- Delivery tracking: reuse existing `webhook_deliveries` pattern (status,
  response code, retry)
- The global `webhook_url` setting continues to work as a fallback for
  backwards compatibility

### 5. Bot Registration Flow

**What:** Define the standard flow for a service to register as a bot in
Sharkfin.

**Details:**
1. Service calls `register` MCP tool with `type: "service"` (or uses REST
   equivalent with Passport service token)
2. Sharkfin creates identity with `type = "service"`, assigns `bot` role
3. Service calls `register_webhook` with its callback URL
4. Service calls `channel_join` for channels it wants to monitor
5. Service receives webhook POSTs for all messages in joined channels
6. Service sends messages via `send_message` with optional `metadata` for
   structured context

## Out of Scope

- Bot-specific UI treatment in the Sharkfin web frontend (can be added later
  using `identity.type` for rendering decisions)
- Rate limiting on bot message sends (add when needed)
- Bot command parsing / routing framework (each service handles its own)
- Sharkbeats (scheduled messages) — separate feature
- Metadata schema validation (Sharkfin stores metadata opaquely)

## Implementation Order

1. Bot role seed (unblocks service identity creation)
2. Per-identity webhook registration (unblocks async message delivery to bots)
3. Webhook recipient scope (unblocks bots receiving channel messages)
4. Message metadata (unblocks structured app-to-app data on messages)

Items 1-3 are blockers for Flow Phase 2b. Item 4 improves extensibility but is
not strictly required — Flow could operate without metadata initially.
