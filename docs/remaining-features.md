# Remaining Features

Tracking document for planned sharkfin features.

## 1. Webhook Notifications ✅

[Plan](plans/2026-02-27-webhook-notifications.md)

POST webhook on mentions and DMs via `--webhook-url` flag.

## 2. Database Abstraction ✅

[Design](2026-03-06-db-abstraction-design.md) · [Plan](plans/2026-03-06-db-abstraction.md)

`domain.Store` interface with SQLite and Postgres backends.

## 3. Encrypted S3 Backup ✅

[Design](2026-03-06-backup-design.md) · [Plan](plans/2026-03-06-backup-implementation.md)

`sharkfin backup {export,import,list}` with age encryption, xz compression,
S3 storage, and `--local` filesystem mode.

## 4. wait_for_messages and Event Bus ✅

[Design](2026-03-10-wait-for-messages-design.md) · [Plan](plans/2026-03-10-wait-for-messages.md)

Domain-level EventBus for decoupled notification delivery. Refactor webhook
firing out of the hub into an EventBus subscriber. Add presence WebSocket
notifications. Implement `wait_for_messages` MCP tool in the bridge that
blocks until unread messages arrive (or timeout), using the presence
notification channel.

## 5. Server Version Query ✅

[Design](2026-03-10-server-version-design.md) · [Plan](plans/2026-03-10-server-version.md)

Expose server version to WS clients (hello envelope + `version` request type)
and MCP clients (serverInfo). Plumb `cmd.Version` through constructors,
replacing hardcoded `"0.1.0"`. Default to `"dev"` when built without ldflags.

## 6. Bridge StreamableHTTP Robustness ✅

[Plan](plans/2026-03-10-bridge-streamablehttp-fixes.md)

Fix the MCP bridge to handle all StreamableHTTP response types: SSE streams
(parse `data:` lines instead of `io.ReadAll`), 202 notification acknowledgments
(skip stdout forwarding), and standard JSON. Addresses pitfalls identified by
the Nexus team in `mcp-integration`.

## 7. Body-Only Mentions ✅

[Design](2026-03-11-mentions-body-only-design.md) · [Plan](plans/2026-03-11-mentions-body-only.md)

Remove explicit `mentions` parameter from `send_message`. All mentions are
extracted server-side from `@username` patterns in the message body.

## 8. Mention Groups ✅

[Design](2026-03-11-mention-groups-design.md) · [Plan](plans/2026-03-11-mention-groups.md)

Named sets of users (e.g. `@backend-team`) that expand to individual mentions
at write time. CRUD operations via WS and MCP. Creator-only management.

## 9. Client Libraries ✅

[Design](2026-03-11-client-libraries-design.md) · [Go Plan](plans/2026-03-11-client-go.md) · [TS Plan](plans/2026-03-11-client-ts.md)

Idiomatic Go and TypeScript WebSocket client libraries. Go client at `client/`
(channel-based events, gorilla/websocket). TypeScript client at `clients/ts/`
published as `@workfort/sharkfin-client` (EventEmitter, zero runtime deps,
Node + browser). Independently versioned via tag prefixes.

## 10. Passport Authentication

[Design](2026-03-13-passport-auth-design.md) · [Plan](plans/2026-03-13-passport-auth.md)

Replace trust-based register/identify with Passport (WorkFort's identity
provider). JWT auth at WS upgrade via JWKS, API key auth for bridge.
`--passport-url` required flag. UUID-based identities, auto-provisioned on
first auth. Client libraries updated to pass tokens at connect time.

## 11. Container Image

Dockerfile for the sharkfin daemon. CI publishes images to Docker Hub and
GitHub Container Registry (ghcr.io) on release. Enables running sharkfin as a
Nexus container.
