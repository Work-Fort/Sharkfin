# Shell Integration Issues — 2026-03-16

Testing Sharkfin web UI integration with the WorkFort shell following the scope team lead's integration test procedure.

**Test environment:**
- Sharkfin daemon: `127.0.0.1:16000`, `--passport-url http://passport.nexus:3000`, `--ui-dir web/dist`, fresh SQLite DB
- Passport: Nexus VM `passport.nexus:3000`, image `ghcr.io/work-fort/passport:v0.0.8`, seeded
- Shell Vite dev server: `localhost:5173`
- BFF: `127.0.0.1:16100`, config at `~/.config/workfort/config.yaml`

---

## Issue 1: BFF cannot discover Passport as auth provider

**Step:** BFF startup / service discovery

**What happened:** The BFF probes every service URL in the fort config at `/ui/health`. Passport (`http://passport.nexus:3000`) returns 404 for `/ui/health` because it has no UI health endpoint. The BFF's `ServiceTracker` silently ignores it. Later, `fort_router.go:141` calls `tracker.ServiceByName("auth")` which returns `false` because Passport was never registered. The `TokenConverter` is `nil`.

**Evidence:**
```
$ curl -s -w "%{http_code}" http://passport.nexus:3000/ui/health
404 Not Found → 404
```

**Impact:** Without a `TokenConverter`, all BFF-proxied API requests (including static UI assets) return:
```json
{"error":"auth: no token converter configured"}
```
with HTTP 401. This blocks `remoteEntry.js` from loading, which blocks the entire Sharkfin UI.

**Root cause:** Passport does not implement the `/ui/health` service discovery endpoint. The BFF expects every service in the config (including non-UI services like auth) to respond to `/ui/health` with a JSON manifest containing at minimum `{"name":"auth"}`.

**Owning service:** Passport

**Proposed fix:** Passport needs a `/ui/health` endpoint returning:
```json
{"status":"ok","name":"auth","label":"Auth","route":""}
```
With HTTP 503 (since it has no UI) — the BFF accepts both 200 and 503 for manifest parsing (`tracker.go:132`). The `name: "auth"` is what `fort_router.go:141` looks for.

**Alternative fix (BFF):** The BFF could exempt `/ui/*` paths from auth when proxying to services, since static assets don't need authentication. This would let `remoteEntry.js` load even without Passport configured.

---

## Issue 2: BFF proxies static UI assets through auth middleware

**Step:** Browser loads `/forts/local/api/sharkfin/ui/remoteEntry.js`

**What happened:** The BFF's handler (`handler.go`) wraps ALL proxied requests with token conversion, including requests to `/forts/{fort}/api/{name}/ui/*`. Static UI assets (`remoteEntry.js`, CSS, JS bundles) are public files that don't need authentication — they're the same as serving a static website.

**Evidence:**
```
$ curl -s http://127.0.0.1:16000/ui/remoteEntry.js | head -c 50
# Direct: 200 OK, returns JS content

$ curl -s http://127.0.0.1:16100/forts/local/api/sharkfin/ui/remoteEntry.js
{"error":"auth: no token converter configured"}
# Via BFF: 401
```

**Impact:** Even if Issue 1 is fixed (Passport discoverable), users who aren't logged in can't even load the shell's UI since the MF remote entry is blocked. The shell needs to load before the user can authenticate.

**Root cause:** `handler.go:98` applies auth to all proxied paths uniformly. No exemption for `/ui/*` static assets.

**Owning service:** Scope (BFF)

**Proposed fix:** The BFF proxy handler should bypass token conversion for paths matching `/forts/{fort}/api/{name}/ui/*`. These are static assets served by the daemon's `http.FileServer` and don't contain user-specific data.

---

## Issue 3: Service discovery works correctly (positive finding)

**Step:** Check `/forts/local/api/services`

**What happened:** Service discovery correctly identified Sharkfin.

**Evidence:**
```json
{
  "fort": "local",
  "services": [{
    "name": "sharkfin",
    "label": "Chat",
    "route": "/chat",
    "enabled": true,
    "ui": true,
    "connected": false
  }],
  "conflicts": []
}
```

**Notes:** `connected: false` is expected since no WS connections are active yet. The `ui: true` confirms the health endpoint is returning the correct manifest and `remoteEntry.js` was found by the tracker.

---

## Issue 4: Shell renders Sharkfin in navigation but shows "unavailable"

**Step:** Navigate to `http://127.0.0.1:16100`

**What happened:** The shell loaded, auto-redirected to `/forts/local/chat`, and rendered the navigation with "Chat" as a service tab. But the main content area shows:

```
Chat is unavailable
This service is not running or has no UI.
```

This is the shell's `Unavailable` fallback component from `service-mount.tsx`. It renders when `loadServiceModule` fails — which it does because `remoteEntry.js` returns 401 (Issue 2).

**Evidence (Playwright snapshot):**
```yaml
- navigation:
  - text: local
  - listitem:
    - img "offline"
    - text: Chat
  - button "Light"
- main:
  - alert:
    - text: Chat is unavailable
    - text: This service is not running or has no UI.
```

**Console errors:**
```
Failed to load resource: /forts/local/api/sharkfin/ui/remoteEntry.js → 404 (Not Found)
Error: [ Federation Runtime ]: Failed to load remote "sharkfin"
```

**Impact:** The Sharkfin UI cannot load at all through the shell.

**Root cause:** Cascading from Issues 1 and 2 — `remoteEntry.js` is blocked by auth.

---

## Issue 5: Sharkfin daemon old DB causes identity provisioning failure

**Step:** Testing JWT auth against the daemon directly

**What happened:** When using an existing SQLite database from a previous version, the WS handler's `UpsertIdentity` call at `ws_handler.go:47` returns an error, producing HTTP 500 with body `identity provisioning failed`. With a fresh database, the same JWT works correctly.

**Evidence:**
```
# Existing DB:
$ curl ws://127.0.0.1:16000/ws (with valid JWT) → 500 "identity provisioning failed"

# Fresh DB:
$ node -e "new WebSocket('ws://...', {headers:{Authorization:'Bearer JWT'}})" → Connected, ping/pong works
```

**Impact:** Upgrading the daemon in production without migrating the DB will break all connections.

**Root cause:** Schema mismatch between old and new DB versions. The daemon's migration system should handle this, but it appears the `identities` table schema changed in a way that `INSERT OR IGNORE` (used by `UpsertIdentity`) fails on the old schema.

**Owning service:** Sharkfin

**Proposed fix:** Investigate the schema diff between old and new DB versions. Ensure migrations run on startup and handle the `identities` table changes gracefully.

---

## Issue 6: Passport JWT uses EdDSA, not RS256

**Step:** Testing JWT auth flow

**What happened:** Not a bug — just documenting. Passport signs JWTs with EdDSA (Ed25519), not RS256. The Sharkfin daemon's JWT validator (`lestrrat-go/jwx/v2`) handles this correctly by matching the `kid` and `alg` from the JWKS. No action needed.

**Evidence:**
```json
JWT header: {"alg":"EdDSA","kid":"9OUS1DK5MaK9jCqT1l3hfFfguYGshaYB"}
JWKS: {"keys":[{"alg":"EdDSA","crv":"Ed25519","kty":"OKP",...}]}
```

The e2e test harness uses RS256 (via `jose` in TS and `lestrrat-go` in Go). This mismatch between test and production should be noted — tests should ideally use the same algorithm as production.

---

## Issue 7: JWT `aud` claim is `http://localhost:3000`, not service-specific

**Step:** Inspecting JWT claims

**What happened:** Passport sets `aud: "http://localhost:3000"` (its own URL) in the JWT. The Sharkfin daemon's JWT validator (`service-auth/jwt/jwt.go`) does NOT validate the `aud` claim — it only validates `exp`/`iat`/`nbf`. This works but is a security consideration: any JWT signed by Passport is accepted by any service, regardless of intended audience.

**Evidence:**
```json
JWT payload: {"aud":"http://localhost:3000","iss":"http://localhost:3000",...}
```

The `iss` is also `http://localhost:3000` rather than a canonical issuer URL, which could cause issues if Passport is accessed via different hostnames (e.g., `passport.nexus:3000` vs `localhost:3000`).

**Owning service:** Passport (issuer configuration) + Passport Go SDK (audience validation)

**Proposed fix:**
1. Passport should use a canonical issuer/audience, not `localhost`
2. The Go SDK should optionally validate `aud` per-service

---

## Issue 8: BFF returns 404 for fort-scoped SPA routes (direct navigation)

**Step:** Navigate directly to `http://127.0.0.1:16100/forts/local/chat`

**What happened:** Direct navigation to `/forts/local/chat` returns 404. The BFF's SPA fallback only handles the root `/` path. Fort-scoped paths go through `fortDispatch` which strips the prefix and forwards to the fort's handler. The fort handler has `/api/*` routes but no SPA fallback for client-side routes like `/chat`.

**Impact:** Refreshing the browser on `/forts/local/chat` or sharing the URL returns 404. Client-side navigation (clicking "Chat" in the nav) works because the SPA router handles it.

**Evidence:**
```
$ curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:16100/forts/local/chat
404

# But clicking "Chat" in the nav works (client-side routing)
```

**Root cause:** `fort_router.go:61` dispatches `/forts/{fort}/{rest...}` to the fort handler, which only has `/api/*` routes. Non-API paths need to be forwarded to the SPA.

**Owning service:** Scope (BFF)

**Proposed fix:** The fort-level handler needs a SPA fallback. Non-API paths under `/forts/{fort}/` should serve the SPA HTML so the client-side router can handle them.

---

## Issue 9: remoteEntry.js uses ES modules but shell loads it as classic script

**Step:** Click "Chat" in the shell nav (client-side navigation)

**What happened:** The shell's MF runtime loads `remoteEntry.js` but fails with "Cannot use import statement outside a module". The `remoteEntry.js` built by Vite uses ES module syntax (`import`/`export`), but the shell's `@module-federation/runtime` loads it as a classic script (no `type="module"`).

**Evidence:**
```
Console error: Cannot use import statement outside a module
Error: [ Federation Runtime ]: Failed to load script resources. #RUNTIME-008
  remoteName: "sharkfin"
  resourceUrl: "http://127.0.0.1:16100/forts/local/api/sharkfin/ui/remoteEntry.js"
```

`remoteEntry.js` line 1:
```javascript
import{i as c}from"./assets/index.cjs-qNTcCtpj.js";
```

**Impact:** The Sharkfin UI cannot load at all — the MF remote fails to initialize.

**Root cause:** `@module-federation/vite` generates ESM output by default. The shell's MF runtime (`@module-federation/runtime`) loads remote entries via dynamic `<script>` tag injection (classic scripts, not `type="module"`). ESM and classic scripts are incompatible.

**Owning service:** Sharkfin (build configuration)

**Proposed fix:** Configure the Vite MF plugin to output a format compatible with the shell's MF runtime. Options:
1. Set `library.type: "global"` or `"system"` in the MF config to output a non-ESM format
2. Check what format the shell's existing remotes use and match it
3. Check if `@module-federation/vite` has a `format` option for the remote entry

This needs investigation — check how the scope team's own MF remotes (if any) configure their Vite builds.

---

## Verification Results (2026-03-16, post-fix)

**Issues 1 and 2: RESOLVED**
- Passport `v0.1.0` returns 503 + auth manifest at `/ui/health`
- BFF discovers both `auth` (ui: false) and `sharkfin` (ui: true)
- `remoteEntry.js` returns 200 through BFF (no more 401)

**New blockers:**
- Issue 9 (ESM format mismatch) blocks the MF remote from loading
- Issue 8 (SPA fallback) is a UX issue for direct URL navigation

---

## Summary

| # | Issue | Severity | Owner | Blocks UI? | Status |
|---|---|---|---|---|---|
| 1 | Passport missing `/ui/health` | High | Passport | Yes (cascading) | **RESOLVED** (v0.1.0) |
| 2 | BFF auth-wraps static UI assets | High | Scope | Yes | **RESOLVED** |
| 3 | Service discovery works | OK | — | — | OK |
| 4 | Shell shows "unavailable" | Cascading | — | Yes (from 9) | Open (new root cause) |
| 5 | Old DB breaks identity provisioning | Medium | Sharkfin | No (fresh DB works) | Non-issue (goose handles it) |
| 6 | EdDSA algorithm (informational) | Low | — | No | Noted |
| 7 | JWT aud/iss uses localhost | Low | Passport | No | Open |
| 8 | BFF 404 on fort-scoped SPA routes | Medium | Scope | No (client nav works) | Open |
| 9 | remoteEntry.js ESM format mismatch | **High** | Scope (shell) | **Yes** | **RESOLVED** — shell now loads remotes as ESM |
| 10 | `wf-button` doesn't submit forms | Medium | Scope (`@workfort/ui`) | No (workaround) | Open |
| 11 | WS constructor fails in browser | **High** | Sharkfin (client) | Yes | **RESOLVED** |
| 12 | MCP bridge has no permissions | Medium | Sharkfin | No (testing only) | Open |

## Issue 10: `wf-button` with `type="submit"` does not trigger form submission

**Step:** Click "Create Account" button in the setup form

**What happened:** Clicking `<wf-button type="submit">` inside a `<form>` does not fire the form's `submit` event. The form handler never runs.

**Root cause:** `wf-button` is a custom element (Lit web component). Custom elements don't participate in native form submission unless they use the `ElementInternals` API with `formAssociated: true`. The `type="submit"` attribute is ignored by the browser for non-native buttons.

**Impact:** Any form using `wf-button` as the submit trigger will silently fail to submit.

**Owning service:** Scope (`@workfort/ui`)

**Workaround:** Use a native `<button type="submit">` styled with `--wf-*` tokens.

**Proposed fix:** Either implement `ElementInternals` in `WfButton` for form association, or document that `wf-button` cannot be used as a form submit button.

---

## Issue 11: SharkfinClient WebSocket constructor fails in browser

**Step:** Sharkfin UI attempts to connect WebSocket after sign-in

**What happened:** `SharkfinClient.connect()` calls `new WebSocket(url, { headers } as any)`. In Node.js (with the `ws` package), the second argument is an options object. In the browser, the native `WebSocket` constructor treats the second argument as subprotocols. Passing `{ headers: {...} }` causes: `Failed to construct 'WebSocket': The subprotocol '[object Object]' is invalid.`

**Impact:** The Sharkfin web UI cannot connect to the daemon via the BFF. `initApp()` throws and the UI shows "Sign in to use Chat" even when the user is authenticated.

**Root cause:** `clients/ts/src/client.ts:70` — `new WS(this.url, { headers } as any)` is not browser-compatible.

**Fix:** Only pass the options object when a custom WebSocket implementation is provided (Node.js) and there are headers to send. Browser connections go through the BFF which handles auth via cookie conversion — no Authorization header needed on the WebSocket.

**Owning service:** Sharkfin (client library)

---

**All blockers resolved.** The Sharkfin MF remote loads in the shell. The UI renders its loading/disconnected state correctly. Full chat functionality requires browser-side auth (user signs in via Passport, BFF converts session to JWT for WS proxy).

### Verification Screenshot

Shell at `/forts/local/chat` shows:
- Navigation: "Auth" + "Chat" tabs
- Sidebar: `wf-skeleton` loading placeholders (SidebarContent rendered)
- Main: `wf-banner` "Chat service is unavailable." (`connected=false` from shell)
- Zero console errors, zero MF load failures

---

## Issue 12: MCP bridge identity has no permissions (chicken-and-egg)

**Step:** Connect Claude Code MCP bridge to Sharkfin daemon and attempt any tool call

**What happened:** Every MCP tool call returns "permission denied". The `capabilities` tool returns `null` (empty permission set). The bridge identity was created when the API key was first used, but since other identities already existed in the database, it did not receive the auto-admin role (which only applies to the very first identity — `identities.go:17-18`).

**Evidence:**
```
mcp__sharkfin__channel_list → "permission denied: channel_list"
mcp__sharkfin__list_roles → "permission denied: manage_roles"
mcp__sharkfin__capabilities → null
```

**Impact:** The MCP bridge cannot be used for integration testing or chat interaction. Admin tools (`set_role`, `grant_permission`, `list_roles`) also require `manage_roles` permission, so the bridge cannot bootstrap itself.

**Root cause:** `mcp_server.go:28-47` — the `toolPermissions` map gates ALL tools including admin tools. `identities.go:16-18` — auto-admin only triggers when `COUNT(*) FROM identities` is 0. The API key in `.mcp.json` (`wf-svcbbt...`) is different from the seeded Passport Sharkfin key (`wf-svceEB...`), so it creates a new identity that gets the default (empty) role.

**Owning service:** Sharkfin

**Proposed fixes (ordered by correctness):**
1. **Use the correct API key** — update `.mcp.json` to use the Passport-seeded Sharkfin service key (`wf-svceEB...`). That identity may already have admin role from being the first service identity. Needs verification.
2. **CLI admin bootstrap** — add a `sharkfin admin grant` CLI command that operates directly on the database, bypassing the permission system. This is the standard pattern for admin bootstrapping.
3. **Exempt admin tools from permission checks for the first admin** — if no identity has `manage_roles`, allow any authenticated identity to use admin tools (bootstrap mode).
