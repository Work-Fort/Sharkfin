# Web UI Gap Analysis — What Was Missed and Why

This document catalogs every gap between the design spec (`docs/web-ui-design.md`), the Storybook mockup, the `@workfort/sharkfin-client` API surface, and the actual implementation.

---

## Critical Gaps

### 1. `client.connect()` is never called

The stores call `client.channels()`, `client.users()`, etc. on a client that was never connected. The `SharkfinClient.connect()` method opens the WebSocket. Without it, every store's initial fetch will throw `"client: not connected"` and hit the `.catch(() => {})` swallowers — producing a blank UI with no error feedback.

**Why missed:** The plan wrote `getClient()` as a lazy singleton but never added a `connect()` call. The tests all use mock clients that don't require connection. The build succeeds because TypeScript doesn't enforce runtime call order. Nobody tested against a real daemon.

**Client API:** `connect(): Promise<void>`

**Fix:** `getStores()` in `web/src/stores/index.ts` must `await client.connect()` before creating stores, or the chat component must connect and gate store creation on connection success.

---

### 2. DMs are broken — hardcoded `'me'` participant filter

`sidebar.tsx:55`: `dm.participants.find((p) => p !== 'me')` — The string `'me'` is never the actual username. Every DM will show the wrong participant or fall back to the first entry (which might be the current user).

**Why missed:** The plan copied this from the design doc's sketch code without thinking about where the current username comes from. The client doesn't expose a "who am I" method, so you need to get it from the shell's auth context or add a self-identity API.

**Client API available:** `capabilities()` returns permissions but not username. The identity comes from Passport via the shell's auth context (`@workfort/auth`). The `SidebarContent` and `SharkfinChat` need access to the current username.

**Fix:** Accept `username` as a prop from the shell (the shell knows the identity), or add a client method to fetch the current user's identity, or call `client.getSettings()` / `client.version()` which might carry identity info.

---

### 3. No create-channel dialog (design spec requirement)

The design spec (line 174) explicitly requires: `Modals | wf-dialog (create channel, invite user)`. The sidebar renders a `+` button that does nothing.

**Why missed:** The plan deliberately excluded modals as "beyond scope" without flagging that the design spec explicitly lists them. The client has `createChannel(name, isPublic)` and `inviteToChannel(channel, username)` — both are available and unused.

**Client API:**
- `createChannel(name: string, isPublic: boolean): Promise<void>`
- `inviteToChannel(channel: string, username: string): Promise<void>`

---

### 4. No invite-user dialog

Same as above. The design spec says `wf-dialog (create channel, invite user)`. There's no UI for inviting users to private channels.

**Client API:** `inviteToChannel(channel, username): Promise<void>`

---

### 5. No DM-open functionality

The sidebar renders DM entries from `dmList()` but there's no way to start a new DM conversation. The client provides `dmOpen(username)` which creates or returns an existing DM channel.

**Why missed:** The plan didn't include any "new DM" UI. The design doc's DM section in the Storybook mockup shows existing DMs but not a create flow, so the plan followed the mockup narrowly rather than the spec's "DMs work" success criterion (line 265).

**Client API:** `dmOpen(username: string): Promise<DMOpenResult>`

---

### 6. No loading states (design spec requirement)

The design spec (line 176) requires: `Loading states | wf-spinner, wf-skeleton`. The implementation shows nothing while channels, messages, and users are loading — it's just blank.

**Why missed:** The plan's store design uses signals that start as empty arrays. There's no `loading` signal, so components can't distinguish "still loading" from "empty." The spec explicitly calls for `wf-skeleton` and `wf-spinner`.

---

### 7. No reconnection handling in the UI

The client supports `reconnect: true` (exponential backoff) and emits `disconnect` and `reconnect` events. The implementation ignores both. When the WebSocket drops, the user sees stale data with no indication that the connection was lost.

**Why missed:** The plan's client wrapper sets `reconnect: true` but never listens for `disconnect`/`reconnect` events. A `connectionState` signal should drive a `wf-banner` showing "Reconnecting..." during disconnects.

**Client events:** `disconnect`, `reconnect`

---

### 8. No responsive/mobile CSS (success criterion 10)

Design spec success criterion 10: "Works on mobile viewport (responsive)." The CSS has no `@media` queries. The sidebar is a fixed 240px column (from the Storybook mockup's grid) — on mobile, the sidebar should collapse or overlay.

**Why missed:** The plan never mentioned responsive design despite the spec requiring it. The Storybook mockup is desktop-only, so the plan followed the mockup literally.

---

## Important Gaps

### 9. Date dividers not rendered

The Storybook mockup shows a "Today" date divider between message groups. The CSS for `.sf-divider` exists but no component renders it. `MessageArea` should group messages by date and insert dividers.

**Why missed:** The plan's `MessageArea` component only handles message grouping by author (continuation), not by date. The CSS was ported verbatim from the mockup, but the component logic wasn't.

---

### 10. Presence doesn't distinguish online/away/offline

The design spec (success criterion 5) says "Presence indicators (online/away/offline) update live." The `User` type has a `state?: string` field (`active`, `idle`), and `PresenceUpdate` has `state?: "active" | "idle"`. The sidebar only checks `online: boolean` for the `wf-status-dot`, ignoring the `state` field entirely. An idle user should show as "away" rather than "online".

**Client types:**
- `User.state?: string` — `'active'` or `'idle'`
- `PresenceUpdate.state?: "active" | "idle"`
- `wf-status-dot` supports `status="away"`

**Fix:** Map `online && state === 'idle'` → `away`, `online && state !== 'idle'` → `online`, `!online` → `offline`.

---

### 11. Search input is non-functional

The sidebar renders a search input but it has no handler. It should filter the channel and DM lists.

**Why missed:** The plan copied the search input from the Storybook mockup but didn't implement filtering logic. This is local filtering (no server API needed) — just filter the `channels` and `dms` signals by the search term.

---

### 12. Channel join not exposed

The client has `joinChannel(channel)` for joining public channels the user isn't a member of. The channel list from `client.channels()` includes `member: boolean` — non-member channels should have a "Join" action.

**Client API:** `joinChannel(channel: string): Promise<void>`

---

### 13. `wf-list` / `wf-list-item` not used for channel/DM lists

The design spec (lines 170-171) specifies `wf-list` and `wf-list-item` for both channel and DM lists. The implementation uses raw `div` elements with CSS classes. This means the lists won't inherit `wf-list`'s keyboard navigation, focus management, or accessibility features.

---

### 14. `wf-divider` not used in channel header

The design spec (line 173) specifies `wf-divider` in the channel header. The implementation uses a bottom border via CSS instead. Minor visual difference but doesn't match the spec.

---

### 15. `setState()` not called — no presence from the web UI

The client has `setState(state: string)` to set the current user's presence state (`active`, `idle`). The web UI never calls it. This means web users appear with no state, and there's no activity tracking (e.g., setting "idle" after inactivity).

**Client API:** `setState(state: string): Promise<void>`

---

## Root Cause Analysis

### Why were these missed?

**1. Plan writing scope-cut without flagging.** When writing Plans 1 and 2, I made implicit decisions to exclude features (modals, DM creation, loading states, responsive CSS, reconnection UI) without marking them as conscious scope reductions against the spec. The design spec has 10 success criteria, and I never checked my plans against them line by line.

**2. Mockup-driven instead of spec-driven.** The plans were designed around "what does the Storybook mockup show?" rather than "what does the design spec require?" The mockup is a static visual reference — it can't show loading states, reconnection behavior, responsive breakpoints, or modal dialogs. The spec covers all of these, but I followed the mockup.

**3. Client API not systematically checked.** The client exports 20+ methods. I should have listed every method and either mapped it to a UI action or explicitly documented why it's not needed. Instead, I cherry-picked the methods I needed for the happy path (channels, history, send, users, unreads, markRead, dmList) and ignored the rest (connect, createChannel, inviteToChannel, joinChannel, dmOpen, setState, capabilities).

**4. `connect()` omission — testing gap.** Every test uses a mock client where methods work without connection. Nobody tested against a real WebSocket. The `connect()` omission would have been caught immediately by running the UI against the daemon.

**5. No end-to-end validation.** The final integration check verified "tests pass and build succeeds" but never loaded the UI in a browser, even in isolation. The success criteria require actual functionality, not just a build artifact.

---

## Client API Coverage

| Client Method | Used? | Gap |
|---|---|---|
| `connect()` | ❌ | Never called — UI is DOA |
| `close()` | ❌ | No cleanup on unmount |
| `channels()` | ✅ | |
| `createChannel()` | ❌ | No create-channel dialog |
| `inviteToChannel()` | ❌ | No invite dialog |
| `joinChannel()` | ❌ | No join UI for non-member channels |
| `sendMessage()` | ✅ | |
| `history()` | ✅ | |
| `unreadMessages()` | — | Not needed (unreadCounts covers it) |
| `unreadCounts()` | ✅ | |
| `markRead()` | ✅ | |
| `dmOpen()` | ❌ | No new-DM UI |
| `dmList()` | ✅ | |
| `users()` | ✅ | |
| `setState()` | ❌ | No presence state management |
| `ping()` | — | Handled by reconnect internally |
| `version()` | — | Not needed for UI |
| `capabilities()` | ❌ | No permission-aware UI |
| Event: `message` | ✅ | |
| Event: `presence` | ✅ | But away state ignored |
| Event: `disconnect` | ❌ | No reconnection UI |
| Event: `reconnect` | ❌ | No reconnection UI |

**Coverage: 8/20 methods used. 2/4 events used.**

---

## Success Criteria Coverage

| # | Criterion | Status | Gap |
|---|---|---|---|
| 1 | Shell loads UI via MF | ✅ | |
| 2 | Channel sidebar in shell's slot | ✅ | |
| 3 | Messages with real-time updates | ⚠️ | Works if connected — but connect() never called |
| 4 | User can send messages | ⚠️ | Same — dead without connect() |
| 5 | Presence indicators update live | ⚠️ | online/offline only, away state ignored |
| 6 | Unread badges update | ⚠️ | Same — dead without connect() |
| 7 | Channel switching works | ✅ | |
| 8 | DMs work | ❌ | Hardcoded 'me', no DM creation |
| 9 | Theming follows --wf-* tokens | ✅ | |
| 10 | Mobile responsive | ❌ | No media queries |

**4/10 criteria fully met. 4/10 partially met. 2/10 not met.**
