# Visual Testing Issues — 2026-03-17

Issues found during manual browser testing of the updated UI.

---

## Issue 1: Hamburger menu content visible alongside button (not hidden in panel)

The `wf-nav-bar`'s `menu` slot content ("Light mode", "Left-handed") renders next to the hamburger button instead of inside the hamburger's slide-out panel. The menu items should only be visible when the hamburger is opened.

**Root cause:** The `menu` slot content is projected into the hamburger element via `appendChild`, but the hamburger component doesn't hide its children when closed — they're always visible.

**Owning:** `@workfort/ui` — `wf-hamburger` needs to hide its children when `open=false`.

---

## Issue 2: Every page reload requires re-signing in

`needsAuth` defaults to `true` on every fresh page load because the session cookie is `HttpOnly` (JavaScript can't read it). The user must sign in on every reload, even though the session is valid.

**Root cause:** No server-side session detection. The BFF needs an endpoint that the shell can probe to check if a valid session exists without requiring auth on the request itself.

**Proposed fix:** Add `GET /api/session` to the BFF that returns `{ authenticated: true/false }` based on the session cookie. The shell probes this on load and sets `needsAuth` accordingly.

**Owning:** Scope BFF + Shell

---

## Issue 3: Permission changes don't take effect without reconnect

After changing a user's role in the database, the Sharkfin UI continues to use the old permissions until the WS connection is reestablished. The permission store is a singleton that fetches `capabilities()` once on `initApp()` and never refreshes.

**Proposed fix:** Either re-fetch capabilities periodically, or expose a "refresh permissions" action.

**Owning:** Sharkfin

---

## Issue 4: Auto-provisioned users get "user" role regardless of Passport role

The `ws_handler.go:43` sets `role := identity.Type` which maps Passport's identity type ("user", "agent", "service") to Sharkfin's RBAC role. A Passport admin is still `type: "user"`, so they get the "user" role in Sharkfin. There's no automatic mapping from Passport admin → Sharkfin admin.

**Current workaround:** Manual `sqlite3` or `sharkfin admin set-role` after first connection.

**Proposed fix:** The first user provisioned on a fresh Sharkfin instance should automatically get the admin role (similar to Passport's setup mode logic).

**Owning:** Sharkfin

---

## Verified Working

- Shell chrome renders with horizontal "Chat" tab, theme icon, hamburger menu
- Sign-in flow works (sign-up → sign-in → session cookie → shell transition)
- Sharkfin MF remote loads, WS connects via BFF cookie→JWT conversion
- Permission system correctly gates UI elements (channels, history, send, DMs)
- Search placeholder shows proper ellipsis character
- Compose input pinned to bottom
- "Chat reconnected" toast appears on WS connect
- Status dot shows green "online" when WS is connected
