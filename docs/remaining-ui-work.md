# Remaining UI Work — WorkFort Platform

Tracks all UI work across the platform. Items are roughly priority-ordered within each section.

---

## Active: Scope Rust Migration (scope-core)

Replacing the Go BFF with a shared Rust library crate. This is the current top priority — it unblocks the Passport admin UI, cross-app notifications, and framework-agnostic MF remotes.

- [Design](2026-03-17-scope-core-design.md) · [Plan](plans/2026-03-17-scope-core-plan.md)

### Phase 1: Workspace + Domain
- [ ] Cargo workspace setup (scope-core, scope-server, workfort-scope)
- [ ] Remove all Go code from scope repo
- [ ] Domain types and Store trait (ports)

### Phase 2: Store Adapters
- [ ] SQLite store adapter (sqlx)
- [ ] Postgres store adapter (sqlx, TIMESTAMPTZ, JSONB)

### Phase 3: Infrastructure
- [ ] HTTP/WS reverse proxy
- [ ] Service discovery + health polling
- [ ] Notification subscriber (connects to service `/notifications/subscribe` WS)
- [ ] YAML config parsing (XDG paths)

### Phase 4: scope-server (axum)
- [ ] API endpoints (forts, session, services, notifications, preferences)
- [ ] Proxy routes + SPA fallback
- [ ] Shell WebSocket (real-time push to browser)

### Phase 5: Tauri Refactor
- [ ] Refactor workfort-scope to use scope-core
- [ ] Native OS notifications via tauri-plugin-notification

### Phase 6: Shell SPA
- [ ] Framework-agnostic service module contract (mount/unmount)
- [ ] Update Sharkfin entry point to mount/unmount
- [ ] Notification bell UI + unread badge
- [ ] Menu display type (hamburger links vs nav tabs)
- [ ] Switch services store from polling to shell WS

### Phase 7: Integration
- [ ] Sharkfin `/notifications/subscribe` endpoint
- [ ] End-to-end verification

---

## Blocked by scope-core: Passport Admin UI

React MF remote providing CRUD for users, service keys, and agent keys. Blocked because it needs admin-only service filtering and the framework-agnostic service mount — both delivered by scope-core.

- [Design](plans/2026-03-17-passport-admin-ui-design.md) · [Plan](plans/2026-03-17-passport-admin-ui-plan.md)

- [ ] Passport: last-admin guard
- [ ] Passport: custom admin API key listing route
- [ ] Passport: /ui/health update (route: "/admin", admin_only: true)
- [ ] Passport: static UI serving at /ui/*
- [ ] Passport: React MF remote scaffold (Vite + Module Federation)
- [ ] Passport: Users page (list, create, edit role, deactivate, delete)
- [ ] Passport: Service Keys page (create, revoke)
- [ ] Passport: Agent Keys page (create, revoke)
- [ ] Sharkfin: identity type → role mapping in auth middleware

---

## Immediate: Bugs to Fix

### Sharkfin Chat
- [ ] Message rendering after send — messages saved to DB but not rendering in the UI until page refresh. Broadcast handler may not be appending to the reactive message list, or the message area isn't updating. Needs systematic debugging.
- [ ] Message input should be disabled when user hasn't joined the selected channel — prevent sending to channels where the user isn't a member. Input should show a disabled state with "Join to send messages" or similar.
- [ ] Auto-join public channels on click — when user clicks a public channel they're not a member of, auto-join if they have `join_channel` permission, then select it
- [ ] `read_public` permission — new RBAC permission allowing reading history of public channels without joining. Default: off (must join to read). When enabled, clicking a public channel shows history in read-only mode with a "Join to send messages" prompt. Sending always requires membership. Add to migration, seed off for all roles. Design principle: configurable with sensible defaults.
- [ ] Remove debug logging from permissions store and chat component (temporary `console.log` calls)
- [ ] Sharkfin daemon should set proper `Cache-Control` headers on UI assets (`no-cache` on `remoteEntry.js`, immutable on content-hashed `assets/*`)

### Shell Chrome
- [ ] Hamburger menu content still visible alongside button on desktop (MutationObserver fix landed but needs verification)
- [ ] Hamburger menu should be hidden on desktop — settings only show inside hamburger panel

### Session / Auth
- [ ] Login form flashes on refresh before session check completes — `needsAuth` defaults to `true`, shows sign-in form, then async `checkSession` hides it. Fix: delay render or default to loading state.

### Integration Issues
- [ ] MCP bridge identity has no permissions (chicken-and-egg — [Issue 12](2026-03-16-shell-integration-issues.md))

---

## Deferred: Storybook (Plan 10)

On hold until scope-core migration is complete and service module contract is finalized.

- [ ] Utility documentation page (initials, time, throttle)
- [ ] ComposeInput stories
- [ ] UserPicker stories
- [ ] NavBar stories at multiple viewports
- [ ] Shell chrome layout stories
- [ ] Sharkfin chat stories (updated with extracted components)
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

### Design Documents (2026-03-17)
- [x] Passport admin UI design — React MF remote, CRUD for users/service keys/agent keys, bootstrap flow
- [x] Scope-core design — Rust migration, shared library crate, notification system, service module contract
- [x] Scope-core implementation plan — 16 tasks across 7 phases

---

## Planned (Future)

### Sharkfin: Mention Groups UI
- [ ] Create/delete mention groups
- [ ] Add/remove members
- [ ] Display in message rendering (@group mentions)

### Sharkfin: Settings UI
- [ ] `setSetting` / `getSettings` exposed in UI
- [ ] User preferences (notification sounds, etc.)

### Passport: Auth Enhancements
- [ ] Sign-in page improvements (social providers when configured)
- [ ] User self-service profile management

### Infrastructure
- [ ] Fort-isolated storage (per-fort AES-256-GCM encryption for localStorage)
- [ ] Gateway architecture (single entry point, scope-core connects to gateway instead of individual services)
- [ ] Sharkfin: proper Cache-Control headers matching scope's `frontend.Handler` pattern
