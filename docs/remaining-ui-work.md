# Remaining UI Work — WorkFort Platform

Tracks all UI work across the platform. Items are roughly priority-ordered within each section.

---

## In Progress: Component Extraction + Shell Chrome

### Component Extraction (Sharkfin → @workfort/ui)
- [ ] Extract utilities to `@workfort/ui`: `initials()`, `formatTime()`, `formatDateLabel()`, `isSameDay()`, `throttle()`
- [ ] Extract `wf-compose-input` web component (from InputBar)
- [ ] Extract `wf-user-picker` web component (from DM/Invite dialogs)
- [ ] Extract `IdleDetector` core class to `@workfort/ui`
- [ ] Extract `useIdleDetection` hook to `@workfort/ui-solid` (+ react/svelte/vue stubs)
- [ ] Extract `PermissionSet` core class to `@workfort/ui`
- [ ] Extract permission hooks per framework adapter
- [ ] Refactor Sharkfin to use extracted components
- [ ] Storybook stories for all extracted components
- [ ] Documentation pages for utilities and hooks

### Shell Chrome Redesign
- [ ] Hide services without UI (like Auth) from nav tabs
- [ ] Horizontal service tabs (not vertical stacked)
- [ ] Hamburger collapse when tabs overflow
- [ ] Configurable hamburger position (any corner)
- [ ] Responsive shell layout (mobile-friendly)
- [ ] Storybook stories for shell chrome at multiple viewports

### Responsive Layouts
- [ ] Shell chrome responsive (hamburger on mobile)
- [ ] Sharkfin chat responsive (sidebar collapse, tight spacing)
- [ ] Storybook stories showing mobile/tablet/desktop states

---

## Planned

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

---

## Done (This Sprint)

- [x] Web UI foundation (Plans 1-2): scaffolding, stores, components, CSS
- [x] Web UI completeness (Plans 3-4): connection lifecycle, identity, permissions, dialogs, search, wf-list migration, responsive CSS, go:embed, Playwright e2e
- [x] Shell integration fixes: Passport /ui/health, BFF auth exemption, ESM module loading
- [x] Onboarding flow: setup mode, sign-up guard, setup form, sign-in form
- [x] Security audit + fixes: 13 vulnerabilities across 3 repos
- [x] Permission gating: join_channel, channel_list, history
- [x] Input bar pinned to bottom
- [x] wf-button form submission (ElementInternals)
- [x] BFF SPA fallback for fort-scoped routes
- [x] SharkfinClient browser WebSocket compatibility
