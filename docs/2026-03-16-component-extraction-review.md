# Component Extraction Review

**Date:** 2026-03-16
**Scope:** Sharkfin web UI (`web/src/`) candidates for `@workfort/ui` or `@workfort/ui-solid`

## Summary

After reviewing the Sharkfin web UI implementation, several utilities and patterns are good candidates for extraction into shared packages. The goal is to identify domain-agnostic code that other WorkFort services could reuse, versus Sharkfin-specific code that should stay in this repo.

---

## Candidates

### 1. `initials()` — `web/src/utils.ts`

**What it does:** Extracts 1-2 character initials from a username string, splitting on `-`, `_`, `.`, or whitespace.

**Domain-agnostic?** Yes. Any service displaying user avatars without images needs this.

**Changes for extraction:** None required — already a pure function with no dependencies.

**Target package:** `@workfort/ui` (framework-agnostic utility)

**Priority:** Extract now. Used in 4 components (Message, DMDialog, InviteDialog, ChannelSidebar). Other services will need identical logic.

---

### 2. `formatTime()` — `web/src/components/message.tsx`

**What it does:** Formats an ISO timestamp to `HH:MM` using `toLocaleTimeString` with `hour12: false`.

**Domain-agnostic?** Yes. Any timestamped feed or log display needs this.

**Changes for extraction:** Move from inline function to exported utility. Consider adding options for 12h/24h preference and locale override.

**Target package:** `@workfort/ui` (framework-agnostic utility)

**Priority:** Extract now. Trivial to extract, high reuse potential.

---

### 3. `formatDateLabel()` — `web/src/components/message-area.tsx`

**What it does:** Returns "Today", "Yesterday", or a full date string (e.g., "March 15, 2026") for date dividers.

**Domain-agnostic?** Yes. Any chronological feed with date grouping needs this pattern (activity logs, notifications, audit trails).

**Changes for extraction:** Move to a shared utility alongside `isSameDay()`. Consider accepting a reference date for testability instead of using `new Date()` internally.

**Target package:** `@workfort/ui` (framework-agnostic utility)

**Priority:** Extract now. The "Today/Yesterday/date" pattern is universal.

---

### 4. `useIdleDetection` — `web/src/hooks/use-idle.ts`

**What it does:** Tracks mouse/keyboard/scroll/visibility events, calls `setState('active')` on activity and `setState('idle')` after 5 minutes. Includes a 30-second throttle on activity events. Returns a cleanup function.

**Domain-agnostic?** Partially. The idle-detection logic (DOM events, throttle, visibility API) is generic. The `SharkfinClient.setState()` call is domain-specific.

**Changes for extraction:**
- Decouple from `SharkfinClient`: accept an `onActive: () => void` and `onIdle: () => void` callback pair instead of a client instance.
- Make `IDLE_TIMEOUT` and throttle interval configurable.
- Extract the internal `throttle()` helper as a standalone utility.

**Target package:** `@workfort/ui` (vanilla JS, no framework dependency). The `throttle()` helper could also go to `@workfort/ui`.

**Priority:** Extract now. Presence/activity tracking is needed by any real-time service.

---

### 5. `Message` — `web/src/components/message.tsx`

**What it does:** Renders a single chat message with avatar (initials), author name, timestamp, and body. Supports a `continuation` mode that hides the avatar and header for consecutive messages from the same author.

**Domain-agnostic?** Partially. The continuation/grouping pattern is common in feeds, but the specific layout (avatar + header + text) is fairly chat-specific. The CSS class naming (`sf-msg`) is Sharkfin-branded.

**Changes for extraction:**
- Rename CSS classes from `sf-` prefix to `wf-message-` or make them configurable via a `classPrefix` prop.
- Accept a render prop or slot for the avatar instead of hardcoding `initials()`.
- Consider making the header content (author + time) composable.

**Target package:** `@workfort/ui-solid` (SolidJS component)

**Priority:** Extract later. The component is small enough that other services may want their own variant. Focus on extracting the utilities it depends on (`initials`, `formatTime`) first.

---

### 6. `MessageArea` — `web/src/components/message-area.tsx`

**What it does:** Scrollable container that renders a list of messages with date dividers and auto-scroll on new messages. Groups consecutive same-author messages as continuations.

**Domain-agnostic?** Partially. The auto-scroll + date-grouping + continuation-grouping pattern applies to any chronological feed. But the specific `Message` type dependency and divider rendering are somewhat domain-coupled.

**Changes for extraction:**
- Generalize the item type: accept a generic `T` with a required `sentAt` (or `timestamp`) field.
- Accept render props for item rendering and divider rendering.
- Extract auto-scroll behavior as a separate `createAutoScroll` primitive.

**Target package:** `@workfort/ui-solid` (SolidJS component)

**Priority:** Extract later. The auto-scroll primitive is worth extracting sooner, but the full component needs more generalization work.

---

### 7. `InputBar` — `web/src/components/input-bar.tsx`

**What it does:** Textarea with Enter-to-send (Shift+Enter for newline) and a send button. Calls `onSend(body)` with trimmed text.

**Domain-agnostic?** Yes. The compose-input pattern (textarea + Enter-to-send + button) is universal for any messaging or commenting UI.

**Changes for extraction:**
- Remove the channel-specific placeholder (`Message #${props.channel}`) — accept a generic `placeholder` prop.
- Replace the hardcoded arrow character with a configurable send icon/label.
- Consider adding `onTyping` callback for typing indicators.

**Target package:** `@workfort/ui-solid` (SolidJS component)

**Priority:** Extract now. This is a clean, self-contained component with minimal domain coupling.

---

### 8. `DMDialog` / `InviteDialog` — `web/src/components/dm-dialog.tsx`, `invite-dialog.tsx`

**What it does:** Both are user-picker dialogs built on `wf-dialog` + `wf-list`. DMDialog filters out the current user and shows presence status dots. InviteDialog shows all users for a given channel.

**Domain-agnostic?** The user-picker pattern is generic, but both components are tightly coupled to Sharkfin's `User` type and presence model.

**Changes for extraction:**
- Extract a generic `UserPickerDialog` that accepts `items: Array<{id, label, avatar?, status?}>` and an `onSelect` callback.
- The status-dot rendering could be a separate `PresenceIndicator` component.
- Domain-specific filtering (exclude self, exclude existing members) stays in the consuming app.

**Target package:** `@workfort/ui-solid` (SolidJS component)

**Priority:** Extract later. The underlying `wf-dialog` + `wf-list` web components already provide the foundation. A generic user-picker built on top would be useful but is lower priority than the utilities.

---

### 9. Permission store — `web/src/stores/permissions.ts`

**What it does:** Calls `client.capabilities()` on creation, stores the result in a `Set<string>` signal, exposes a `can(permission)` boolean check.

**Domain-agnostic?** The `capabilities() -> can()` pattern is generic. The store is coupled to `SharkfinClient` for the initial fetch, but the storage and lookup pattern is universal.

**Changes for extraction:**
- Accept a `fetchPermissions: () => Promise<string[]>` function instead of a `SharkfinClient`.
- The resulting `{ can, permissions }` API is reusable by any service with capability-based access control.

**Target package:** `@workfort/ui-solid` (SolidJS reactive store)

**Priority:** Extract now. Permission-gated UI is needed across all WorkFort services. The pattern is small, clean, and easy to generalize.

---

## Extraction Priority Summary

### Extract now (low effort, high reuse)

| Candidate | Target | Effort |
|---|---|---|
| `initials()` | `@workfort/ui` | Trivial — copy as-is |
| `formatTime()` | `@workfort/ui` | Trivial — add options |
| `formatDateLabel()` + `isSameDay()` | `@workfort/ui` | Trivial — add ref-date param |
| `useIdleDetection` | `@workfort/ui` | Small — decouple from client |
| `throttle()` | `@workfort/ui` | Trivial — already standalone |
| `InputBar` | `@workfort/ui-solid` | Small — generalize placeholder |
| Permission store | `@workfort/ui-solid` | Small — accept fetch fn |

### Extract later (needs generalization)

| Candidate | Target | Effort |
|---|---|---|
| `Message` | `@workfort/ui-solid` | Medium — composable slots |
| `MessageArea` | `@workfort/ui-solid` | Medium — generic types + render props |
| `DMDialog` / `InviteDialog` | `@workfort/ui-solid` | Medium — generic user picker |

## Next Steps

1. Create `@workfort/ui/src/format.ts` with `initials`, `formatTime`, `formatDateLabel`, `isSameDay`.
2. Create `@workfort/ui/src/idle.ts` with generic `createIdleDetector(onActive, onIdle, opts)`.
3. Create `@workfort/ui-solid/src/input-bar.tsx` with generic `ComposeInput` component.
4. Create `@workfort/ui-solid/src/permissions.ts` with generic `createPermissionStore(fetchFn)`.
5. Update Sharkfin web UI to import from shared packages instead of local copies.
