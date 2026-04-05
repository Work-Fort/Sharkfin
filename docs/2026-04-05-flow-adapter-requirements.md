# Flow Adapter Requirements

## Purpose

Flow (workflow engine) needs to integrate with Sharkfin as a chat adapter.
This document captures what Sharkfin needs to provide for Flow's integration
to work.

## What Flow Needs

Flow's Sharkfin adapter will:
- Register as a service/bot identity
- Send messages to channels with metadata (state change notifications)
- Create and join channels
- Receive messages via per-identity webhooks
- Reply to messages using thread_id

Flow will call Sharkfin via REST API endpoints (not MCP or WebSocket).
Inbound messages arrive via per-identity webhooks.

## Requirements

### 1. REST API Endpoints

Sharkfin currently exposes functionality through MCP tools and WebSocket.
Flow needs REST endpoints for service-to-service communication. REST is
the standard integration pattern across WorkFort services — it works with
service meshes, is observable, and doesn't require persistent connections.

**Required endpoints:**

**Messaging:**
- `POST /api/v1/channels/{channel}/messages` — send a message
  ```json
  {
    "body": "Work item #42 moved to Code Review",
    "metadata": {"event_type": "work_item_transitioned", "event_payload": {...}},
    "thread_id": 123
  }
  ```
  Response: 201 with message object (id, body, metadata, sent_at)

- `GET /api/v1/channels/{channel}/messages` — list messages (pagination)
  Query params: `?before=`, `?after=`, `?limit=`

**Channels:**
- `POST /api/v1/channels` — create channel
  ```json
  {
    "name": "flow-nexus-sdlc",
    "public": true
  }
  ```
  Response: 201 with channel object

- `GET /api/v1/channels` — list channels

- `POST /api/v1/channels/{channel}/join` — join a channel

**Webhooks:**
- `POST /api/v1/webhooks` — register webhook for calling identity
  ```json
  {
    "url": "http://flow:17200/v1/webhooks/sharkfin",
    "secret": "shared-secret"
  }
  ```
  Response: 201 with webhook object (id, url, active)

- `GET /api/v1/webhooks` — list caller's webhooks

- `DELETE /api/v1/webhooks/{id}` — unregister webhook

**Identity:**
- `POST /api/v1/auth/register` — register service identity
  ```json
  {
    "username": "flow-bot",
    "type": "service"
  }
  ```

All endpoints authenticated via Passport (`Authorization: Bearer <token>`).

### 2. Go Client Library Updates

Update the Go client library (`client/`) to use the new REST endpoints
instead of WebSocket for these operations. This makes the client usable
by services that don't need persistent connections.

**Required additions to the client:**

```go
// Messaging
func (c *Client) SendMessage(ctx, channel, body string, opts *SendOpts) (int64, error)
// SendOpts gains Metadata field:
type SendOpts struct {
    ThreadID *int64
    Metadata *string
}

// Channels
func (c *Client) CreateChannel(ctx, name string, public bool) error
func (c *Client) JoinChannel(ctx, channel string) error
func (c *Client) ListChannels(ctx) ([]Channel, error)

// Webhooks
func (c *Client) RegisterWebhook(ctx, url, secret string) (string, error)
func (c *Client) UnregisterWebhook(ctx, id string) error
func (c *Client) ListWebhooks(ctx) ([]Webhook, error)
```

The client can either use REST directly or continue using WebSocket
internally — that's an implementation detail. What matters is that the
public API covers these operations.

## Summary

| Item | Scope | Blocker? |
|------|-------|----------|
| REST API endpoints | Server-side handlers | Yes |
| Client library updates (metadata, webhooks, channels) | Client library | Yes |

## Context

These REST endpoints follow the same pattern used by Hive (`/v1/agents`,
`/v1/roles`, etc.) and Combine (`/api/v1/repos/{repo}/issues`). Every
WorkFort service exposes its functionality via REST for service-to-service
communication. MCP is the agent-facing interface; REST is the
service-facing interface.
