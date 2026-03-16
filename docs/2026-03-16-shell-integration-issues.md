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

## Summary

| # | Issue | Severity | Owner | Blocks UI? |
|---|---|---|---|---|
| 1 | Passport missing `/ui/health` | High | Passport | Yes (cascading) |
| 2 | BFF auth-wraps static UI assets | High | Scope | Yes |
| 3 | Service discovery works | OK | — | — |
| 4 | Shell shows "unavailable" | Cascading | — | Yes (from 1+2) |
| 5 | Old DB breaks identity provisioning | Medium | Sharkfin | No (fresh DB works) |
| 6 | EdDSA vs RS256 (informational) | Low | — | No |
| 7 | JWT aud/iss uses localhost | Low | Passport | No |

**Blockers for shell integration:** Issues 1 and 2 must be resolved. Either Passport adds `/ui/health` OR the BFF exempts `/ui/*` from auth. Ideally both.
