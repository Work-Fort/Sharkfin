# Remaining Work — Sharkfin

Tracks remaining work for the sharkfin project. Items are roughly priority-ordered within each section.

---

## Open

### Bugs
- [ ] ⚠️ **NEEDS PLAN** `read_public` permission — new RBAC permission, migration, seed, UI changes for read-only mode with join prompt. Touches daemon, client, and web UI.
- [ ] **Postgres migration bool/int mismatch** — `INSERT INTO roles (name, built_in) VALUES ('bot', 1)` is rejected by Postgres with `column "built_in" is of type boolean but expression is of type integer at character 51`. SQLite tolerates the integer-as-boolean coercion; Postgres does not. Bug surfaces every time the `e2e-postgres` job runs (CI run 24615943838 and Release run 24615943841 on push 2667cb59 both failed here, as did the prior push c8797ed). Fix: rewrite the affected migration to use `TRUE` / `FALSE` literals — or whatever `bool` form is portable across both backends — so the same migration succeeds against both stores. Likely `internal/infra/postgres/migrations/` and the matching `internal/infra/sqlite/migrations/` (verify by grep for `built_in`).
- [ ] **Flaky e2e: `TestPresenceNoNotificationWithoutMention`** — fails intermittently with `channel_create: rpc=&{Code:-32603 Message:create channel: insert channel: constraint failed: UNIQUE constraint failed: channels.name (2067)}`. Indicates per-test isolation drift — a prior subtest's channel name leaks into a later subtest's create. Either subtests share a daemon (and need unique channel names per subtest), or cleanup between subtests is incomplete. Reproduces in `mise run e2e` runs ~CI-frequency.
- [ ] **Flaky e2e: `TestWSMentionGroupExpansion`** — fails with `send: env={Type:error D:[…"you are not a participant of this channel"…]}`. Suggests the test sends a message before completing channel join, or the join was performed against a different daemon instance than the send. Same reproduction frequency as the Presence flake — both surface in the same e2e run.
- [ ] MCP bridge identity has no permissions (chicken-and-egg). Now solvable via the admin UI (create agent key). Note: likely not needed as manual docs — the process is being fully automated soon.
- [ ] **Release task missing `-tags ui`** — `.mise/tasks/build/release` runs `go build` without `-tags ui`, producing a binary that 404s on `/ui/*` (no embedded frontend). Broke Scope's chat view on first integration-env bring-up until someone rebuilt manually. Either default release to `-tags ui`, or split into `release-cli` (no UI) and `release` (with UI) and make the default include the UI.

---

## Deferred: Storybook (Plan 10)

Storybook lives in `~/Work/WorkFort/documentation/storybook/` (Lit on port 6006, Solid on port 6007, React on port 6008). Component extraction is complete — these stories can now be written.

- [ ] Utility documentation page (initials, time, throttle)
- [ ] ComposeInput stories
- [ ] UserPicker stories
- [ ] Avatar stories (sm, md, with/without status dot)
- [ ] NavBar stories at multiple viewports
- [ ] NavSidebar stories (with sections, search, badges)
- [ ] Hamburger stories
- [ ] Shell chrome layout stories
- [~] Sharkfin chat stories — `SharkfinChat.stories.ts` exists as the original UI prototype; needs updating with extracted components (`wf-avatar`, `wf-divider label`, `wf-nav-sidebar`)
- [ ] Hook documentation pages (IdleDetector, Permissions per framework)

---

## Test Coverage Gaps

### Convention: every `t.Skip` must be cross-referenced here

Any conditional `t.Skip` in an e2e or integration test MUST have a corresponding
entry in this section. The entry must name the test, state the condition under
which it skips, and describe the work needed to remove the skip.

A skip with no paper trail is indistinguishable from an accidental omission — and
will be treated as one during future audits. The rationale for this rule is
documented in the architecture reference:

> See `skills/lead/go-service-architecture/references/architecture-reference.md`
> §"Multi-Daemon Test Isolation (Per-Backend)" for the harness pattern and
> the anti-pattern that created this gap.

### Resolved: `TestBackupExportImport` skip (SHARKFIN_DB)

**Status: closed.** The skip was removed in commits `8fa7499` and `57820bb`
(2026-04-18). The test now runs on both SQLite and Postgres using the
`harness.AltDB(t)` helper added in `8fa7499`.

**Historical context:** The test was introduced in commit `c5a1f5f` (2026-03-06)
with an inline skip (`t.Skip("backup e2e requires SQLite")`) that activated
whenever `SHARKFIN_DB` was set. The skip was never mentioned in the commit
message, the implementation plan (`docs/plans/2026-03-06-backup-implementation.md`),
or this file. The CI e2e-postgres job therefore silently skipped the only
end-to-end backup verification on every PG run for six weeks.

The fix required adding `harness.AltDB(t)` — a primitive that provisions a
sibling database per daemon, abstracting over the SQLite/Postgres difference.
Without that primitive the two-daemon test shape is inherently SQLite-only; the
right response to that harness gap is to add the primitive, not to skip the test.

---

## Recently Completed

### Component Extraction (Plans 5-8)
- [x] Extract utilities to `@workfort/ui`: `initials()`, `formatTime()`, `formatDateLabel()`, `isSameDay()`, `throttle()`
- [x] Extract `wf-compose-input` web component
- [x] Extract `wf-user-picker` web component
- [x] Extract `IdleDetector` core class to `@workfort/ui`
- [x] Extract `PermissionSet` core class to `@workfort/ui`
- [x] Extract `useIdleDetection` + `usePermissions` hooks to all 4 framework adapters (solid, react, svelte, vue)
- [x] Refactor Sharkfin to use extracted components

### Shell Chrome Redesign (Plan 9)
- [x] `wf-hamburger` component with configurable position
- [x] `wf-nav-bar` component with overflow detection
- [x] Shell updated to use new components
- [x] Non-UI services hidden from nav tabs
- [x] Handedness preference (left/right)
- [x] Responsive grid — sidebar overlay on mobile

### Onboarding (Plan 11 partial)
- [x] Passport: `setup_mode` flag in `/ui/health`
- [x] Passport: sign-up guard (admin only after first user)
- [x] Passport: migrations on startup
- [x] Passport: `TRUSTED_ORIGINS` env var
- [x] BFF: `setup_mode` passthrough in service tracker
- [x] BFF: session probe endpoint (`GET /api/session`)
- [x] Shell: setup form (create admin account)
- [x] Shell: sign-in form (returning users)
- [x] Shell: session persistence across page reloads

### Bug Fixes
- [x] `wf-button` form submission via ElementInternals
- [x] BFF SPA fallback for fort-scoped routes
- [x] SharkfinClient WebSocket browser compatibility
- [x] Permission reactivity — `createMemo` + module-level signal + `await capabilities`
- [x] First user auto-admin role
- [x] Capabilities refetch on reconnect
- [x] General channel seeded on first startup
- [x] Search placeholder ellipsis rendering
- [x] Input bar pinned to bottom

### Security Fixes (13 total)
- [x] Sharkfin: WS history membership check, WS dm_list scoping, WS origin validation, WipeAll allowlist
- [x] Passport: BETTER_AUTH_SECRET startup guard, sign-up admin role check, DB error handling, log sanitization
- [x] Scope: WS origin validation, HTTP server timeouts, cookie HttpOnly/SameSite, token cache eviction, UI asset whitelist

### Flow Adapter REST API + Client Reorg (2026-04-05)
- [x] REST API endpoints: 9 routes for service-to-service communication (messages, channels, webhooks, identity)
- [x] Go client library: REST methods for webhooks, identity registration, metadata on SendOpts
- [x] Webhook `secret` field removed (migration 013 + code cleanup)
- [x] Request body size limits (1 MiB MaxBytesReader) on all REST handlers
- [x] Client directory reorganized: `client/go/`, `client/ts/` (was `client/` + `clients/ts/`)
- [x] Go module path: `github.com/Work-Fort/sharkfin/client/go`
- [x] Apache-2.0 licensing on client/go, client/ts, and web (SPDX headers + LICENSE.md)
- [x] Auto-tag release workflows for both Go and TS clients (path-based triggers, mirrors Passport pattern)
- [x] Main release workflow excludes client paths to prevent double-triggering

### Bot/Service Identity (2026-04-05)
- [x] `bot` role migration with scoped permissions (send_message, create_channel, join_channel, history, channel_list, unread_messages, unread_counts, mark_read, dm_list, dm_open, user_list)
- [x] Auto-assign `bot` role when `identity.Type == "service"` in `UpsertIdentity`
- [x] Per-identity webhook registration (`register_webhook`, `unregister_webhook`, `list_webhooks` MCP tools)
- [x] `identity_webhooks` table with UNIQUE(identity_id, url) deduplication
- [x] Webhook dispatch to service channel members (`GetWebhooksForChannel`, `GetServiceMemberUsernames`)
- [x] `WebhookPayload` extended with `channel_id`, `channel_name`, `from_type`, `metadata`
- [x] Message metadata — optional `metadata TEXT` column, threaded through store → handler → broadcast → webhook
- [x] Integration test for full bot registration flow with threading convention
- [x] Requirements doc updated: replaced `kind` enum with metadata JSON sidecar (Slack-informed design)

### Infrastructure (2026-03-19)
- [x] Passport VM recreated with persistent `/data` drive — SQLite DB survives image upgrades
- [x] Passport restart policy set to `always`

### UI Cleanup + Component Extraction (Plans A/B/C, 2026-03-19)
- [x] Token cleanup: `--wf-text-2xs`, all hardcoded styles replaced with tokens, raw inputs → `wf-input`
- [x] `wf-avatar` component: initials + optional status dot (sm/md sizes)
- [x] `wf-divider` label extension: optional centered text between lines
- [x] `wf-nav-sidebar` + `wf-nav-section` components: slot-based navigation sidebar with collapsible sections and search
- [x] Sharkfin refactored to use all extracted components
- [x] `wf-user-picker` refactored to use `wf-avatar`

### Bug Fixes (2026-03-19)
- [x] Sidebar not unmounted when navigating away from service — `ServicePage.onCleanup` now clears sidebar signal; `ShellLayout` tracks previous sidebar and calls `unmount()` on transition, not just on full layout teardown

### Bug Fixes (2026-03-18)
- [x] Login form flash on refresh — `sessionChecked` gate signal shows "Loading…" until session probe resolves
- [x] Hamburger visible on desktop — `wf-hamburger[hidden] { display: none }` CSS fix
- [x] Message input enabled when not joined — membership check + "Join to send" placeholder
- [x] Connection lost banner flashing ~1/sec — debounced disconnect signal (2s stability before hide)
- [x] `\u2026` rendering literally in banner — replaced with real `…` character
- [x] Debug console.log in permissions effect — removed
- [x] Cache-Control headers on sharkfin UI assets — `no-cache` for entry, `immutable` for hashed assets
- [x] Sidebar background color mismatch — changed from `--wf-color-bg-secondary` to `--wf-color-bg`

### Stable Identity ID (2026-03-18)
- [x] Migration 008: `auth_id` column on identities table (SQLite + Postgres)
- [x] `UpsertIdentity` resolves by `auth_id` first, then `username` fallback
- [x] Internal UUID generation — Passport ID changes no longer break FK references
- [x] Warning log when Passport auth_id changes for existing user
- [x] Daemon handlers (WS, MCP, notifications) updated to use returned `*Identity`
- [x] Backup import updated for new `UpsertIdentity` signature
- [x] Test suite fixed for seeded "general" channel and first-user auto-admin

### Design Documents (2026-03-17)
- [x] Passport admin UI design — React MF remote, CRUD for users/service keys/agent keys, bootstrap flow
- [x] Scope-core design — Rust migration, shared library crate, notification system, service module contract
- [x] Scope-core implementation plan — 16 tasks across 7 phases

### Design Documents (2026-03-19)
- [x] Token cleanup plan — replace hardcoded styles with design tokens
- [x] Component extraction plan — `wf-avatar`, `wf-divider` label extension
- [x] Nav sidebar extraction plan — `wf-nav-sidebar` + `wf-nav-section` for shared use across MF remotes

### Core Features (pre-2026-03-17)
- [x] Webhook notifications — POST webhook on mentions and DMs
- [x] Database abstraction — `domain.Store` interface with SQLite and Postgres backends
- [x] Encrypted S3 backup — age encryption, xz compression, S3 + local mode
- [x] Event bus — domain-level EventBus, decoupled webhook delivery, `wait_for_messages` MCP tool
- [x] Server version query — WS hello envelope + `version` request + MCP serverInfo
- [x] Bridge StreamableHTTP robustness — SSE streams, 202 acknowledgments, standard JSON
- [x] Body-only mentions — server-side @mention extraction from message body
- [x] Mention groups — named user sets (`@backend-team`), CRUD via WS and MCP
- [x] Client libraries — Go (`client/go/`) and TypeScript (`client/ts/`) WebSocket clients
- [x] Passport authentication — JWT via JWKS, API key auth, UUID-based identities
- [x] Container image — Dockerfile, multi-arch builds, GHCR publishing on release

---

## Planned (Future)

### Sharkfin: Mention Groups UI
- [ ] Create/delete mention groups
- [ ] Add/remove members
- [ ] Display in message rendering (@group mentions)

### Sharkfin: Identity Username Decoupling
- [ ] Service/bot identities are tracked by username throughout webhook dispatch, channel membership, and recipient resolution. Username changes would break these associations. Track by internal identity ID instead, so usernames can be renamed in sharkfin without reissuing Passport service keys or updating external config.

### Sharkfin: Settings UI
- [ ] `setSetting` / `getSettings` exposed in UI
- [ ] User preferences (notification sounds, etc.)


---

## Deferred

### Service "On Behalf Of" Authorization
- [ ] Design doc: mechanism for services to act on behalf of a specific user (e.g., send a message as User X, access a private channel scoped to User Y). The `bot` role and type→role mapping are implemented; this is the remaining cross-user delegation model. No concrete use case yet — defer until a service needs it.

