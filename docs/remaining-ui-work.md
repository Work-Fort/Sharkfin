# Remaining UI Work — WorkFort Platform

Tracks all UI work across the platform. Items are roughly priority-ordered within each section.

---

## Active: Scope Rust Migration (scope-core)

Replacing the Go BFF with a shared Rust library crate. Phases 1-4, 6, and 7 are complete. Phase 5 (Tauri) is deferred.

- [Design](2026-03-17-scope-core-design.md) · [Plan](plans/2026-03-17-scope-core-plan.md)

### Phase 1: Workspace + Domain
- [x] Cargo workspace setup (scope-core, scope-server, workfort-scope)
- [x] Remove all Go code from scope repo
- [x] Domain types and Store trait (ports)

### Phase 2: Store Adapters
- [x] SQLite store adapter (sqlx)
- [x] Postgres store adapter (sqlx, TIMESTAMPTZ, JSONB)

### Phase 3: Infrastructure
- [x] HTTP/WS reverse proxy
- [x] Service discovery + health polling
- [x] Notification subscriber (connects to service `/notifications/subscribe` WS)
- [x] YAML config parsing (XDG paths)

### Phase 4: scope-server (axum)
- [x] API endpoints (forts, session, services, notifications, preferences)
- [x] Proxy routes + SPA fallback
- [x] Shell WebSocket (real-time push to browser)

### Phase 5: Tauri Refactor
- [ ] Refactor workfort-scope to use scope-core
- [ ] Native OS notifications via tauri-plugin-notification

### Phase 6: Shell SPA
- [x] Framework-agnostic service module contract (mount/unmount)
- [x] Update Sharkfin entry point to mount/unmount
- [x] Notification bell UI + unread badge
- [x] Menu display type (hamburger links vs nav tabs)
- [x] Switch services store from polling to shell WS

### Phase 7: Integration
- [x] Sharkfin `/notifications/subscribe` endpoint
- [x] End-to-end verification

---

## Passport Admin UI

React MF remote providing CRUD for users, service keys, and agent keys. Functional and deployed — uses scope-core's service mount and admin-only filtering.

- [Design](plans/2026-03-17-passport-admin-ui-design.md) · [Plan](plans/2026-03-17-passport-admin-ui-plan.md)

- [x] Passport: last-admin guard
- [x] Passport: custom admin API key listing route
- [x] Passport: /ui/health update (route: "/admin", admin_only: true)
- [x] Passport: static UI serving at /ui/*
- [x] Passport: React MF remote scaffold (Vite + Module Federation)
- [x] Passport: Users page (list, create, edit role, deactivate, delete)
- [x] Passport: Service Keys page (create, revoke)
- [x] Passport: Agent Keys page (create, revoke)
- [ ] ⚠️ **NEEDS DESIGN + PLAN** Sharkfin: service identity permissions — service-to-service auth requires its own RBAC role with cross-user operation support, distinct from user/agent row-level scoping. Currently `identity.Type` maps directly to role name, which breaks for "service" type (no matching role). Needs design doc covering permission set, "on behalf of" semantics, and scoping rules.

---

## Immediate: Bugs to Fix

### Sharkfin Chat
- [x] Message rendering after send — hub skipped broadcasting to the sender; removed the sender-skip guard so the existing message.new → signal → render pipeline works for all clients.
- [x] "Connection lost" banner flashing ~1/sec — debounced: banner shows immediately on disconnect, hides only after connection stable for 2s. Fixed `\u2026` literal rendering (now real `…` character).
- [x] Message input should be disabled when user hasn't joined the selected channel — check membership state, disable input + show "Join to send messages" placeholder.
- [x] Auto-join public channels on click — when user clicks a public channel they're not a member of, auto-join if they have `join_channel` permission, then select it.
- [ ] ⚠️ **NEEDS PLAN** `read_public` permission — new RBAC permission, migration, seed, UI changes for read-only mode with join prompt. Touches daemon, client, and web UI.
- [x] Remove debug logging from permissions store and chat component (temporary `console.log` calls).
- [x] Sharkfin daemon should set proper `Cache-Control` headers on UI assets (`no-cache` on `remoteEntry.js`, immutable on content-hashed `assets/*`).

### Shell Chrome
- [x] Hamburger menu content still visible alongside button on desktop — `wf-hamburger[hidden] { display: none }` added (CSS `display: block` was overriding `hidden` attribute).

### Session / Auth
- [x] Login form flashes on refresh before session check completes — added `sessionChecked` gate signal; shows "Loading…" until session probe resolves.

### Integration Issues
- [ ] MCP bridge identity has no permissions (chicken-and-egg — [Issue 12](2026-03-16-shell-integration-issues.md)). Now solvable via the admin UI (create agent key), but the bootstrap flow needs documenting.

---

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

---

## Planned (Future)

### Sharkfin: Mention Groups UI
- [ ] Create/delete mention groups
- [ ] Add/remove members
- [ ] Display in message rendering (@group mentions)

### Sharkfin: Settings UI
- [ ] `setSetting` / `getSettings` exposed in UI
- [ ] User preferences (notification sounds, etc.)

### Sharkfin: Service Identity Permissions
- [ ] Design doc: service-to-service authorization model (cross-user operations, "on behalf of" semantics, permission scoping)
- [ ] New "service" RBAC role with dedicated permission set
- [ ] Identity type → role mapping in auth middleware (user→user, agent→agent, service→service)

### Passport: Auth Enhancements
- [ ] Sign-in page improvements (social providers when configured)
- [ ] User self-service profile management

### Infrastructure
- [ ] Fort-isolated storage (per-fort AES-256-GCM encryption for localStorage)
- [ ] Gateway architecture (single entry point, scope-core connects to gateway instead of individual services)
