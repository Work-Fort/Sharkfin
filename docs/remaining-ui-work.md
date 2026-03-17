# Remaining UI Work — WorkFort Platform

Tracks all UI work across the platform. Items are roughly priority-ordered within each section.

---

## Immediate: Bugs to Fix

### Sharkfin Chat
- [ ] Auto-join public channels on click — when user clicks a public channel they're not a member of, join automatically instead of showing error
- [ ] Message rendering after send — messages saved to DB but not rendering in the UI (broadcast handler may not be appending, or message area component not showing messages)
- [ ] Remove debug logging from permissions store and chat component (temporary `console.log` calls)
- [ ] Sharkfin daemon should set proper `Cache-Control` headers on UI assets (`no-cache` on `remoteEntry.js`, immutable on content-hashed `assets/*`)

### Shell Chrome
- [ ] Hamburger menu content still visible alongside button on desktop (MutationObserver fix landed but needs verification)
- [ ] Hamburger menu should be hidden on desktop — settings only show inside hamburger panel

### Session / Auth
- [ ] BFF logout endpoint — clear session cookie
- [ ] Session probe should also work after fort change (reset + re-probe)

---

## In Progress: Storybook (Plan 10)

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

---

## Planned (Future)

### Cross-App Notification System
- [ ] Design: shell-wide notification mechanism for cross-app alerts
- [ ] Notification sound support
- [ ] Notification UI (not just toasts — persistent attention-getting)
- [ ] Example: Sharkfin message notification while user is in Hive

### Sharkfin: Mention Groups UI
- [ ] Create/delete mention groups
- [ ] Add/remove members
- [ ] Display in message rendering (@group mentions)

### Sharkfin: Settings UI
- [ ] `setSetting` / `getSettings` exposed in UI
- [ ] User preferences (notification sounds, etc.)

### Passport: Auth UI
- [ ] Sign-in page improvements (social providers when configured)
- [ ] User management admin panel (future)

### Infrastructure
- [ ] Nexus: document Bug 4 (env vars) — RESOLVED in latest version
- [ ] Nexus: document Bug 5 (script_override) — may be resolved
- [ ] Sharkfin: proper Cache-Control headers matching scope's `frontend.Handler` pattern
