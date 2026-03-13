# Passport Authentication Integration Design

Replace Sharkfin's trust-based register/identify system with Passport
(WorkFort's identity provider) using JWT and API key authentication.

## Auth Flow

Passport's Go SDK (`github.com/Work-Fort/Passport/go/service-auth`)
provides production-ready middleware. Three integration points:

**WebSocket (`/ws`, `/presence`):** Passport middleware runs at HTTP upgrade
time. The SDK extracts `Authorization: Bearer <jwt>` from the upgrade
request, validates against JWKS, and stores `auth.Identity` in the request
context. The hub reads identity from context during connection setup. JWT is
validated once — the connection is trusted for its lifetime.

**MCP (`/mcp`):** Passport middleware runs per HTTP request. Same JWT
validation. Each StreamableHTTP request carries its own Bearer token.

**MCP Bridge:** Authenticates via API key. The bridge passes
`Authorization: Bearer <api-key>` on each request (same header format
as JWT — the SDK middleware dispatches to the APIKeyValidator which
POSTs to `/v1/verify-api-key` with a 30-second cache).

**Configuration:** `--passport-url` is a required flag. The server refuses to
start without it. The SDK uses this to derive endpoints:
- JWKS: `<passport-url>/v1/jwks` (auto-refreshes every 20 minutes)
- API key verification: `<passport-url>/v1/verify-api-key`

**Middleware setup:**
```go
opts := auth.DefaultOptions(passportURL)
jwtV := auth.NewJWTValidator(opts)
akV  := auth.NewAPIKeyValidator(opts)
mw   := auth.NewFromValidators(jwtV, akV)
```

All three endpoints (`/ws`, `/mcp`, `/presence`) are wrapped with `mw`.
Identity is extracted via `auth.IdentityFromContext(r.Context())`.

## Identity Model

Drop the `users` table. Replace with `identities`:

```sql
CREATE TABLE identities (
    id           TEXT PRIMARY KEY,  -- Passport UUID
    username     TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT 'user'
);
```

All foreign keys change from `user_id INTEGER` to `identity_id TEXT`:
- `messages`, `channel_members`, `mentions`, `dm_participants`,
  `read_positions`, `settings`, `mention_groups` (`created_by`),
  `mention_group_members`

Domain type:

```go
type LocalIdentity struct {
    ID          string // Passport UUID
    Username    string
    DisplayName string
    Type        string // "user", "agent", "service"
}
```

**Auto-provisioning:** On first authenticated request, if the Passport UUID
doesn't exist in `identities`, insert it from the JWT claims
(`sub`, `username`, `display_name`, `type`). Uses `INSERT OR IGNORE` to
handle concurrent first-connections safely.

**Existing database migration:** Manual one-off task (not part of this
feature). Fresh installs create `identities` directly.

## What Gets Removed

- **SessionManager** — no sessions to track; identity comes from JWT
- **register handler** (WS + MCP) — Passport manages user creation
- **identify handler** (WS + MCP) — identity comes from middleware
- **get_identity_token** (MCP) — bridge uses API key instead
- **`users` table and Store methods** — replaced by `identities`
- **Password fields** — Passport owns credentials
- **Hello envelope** — connection is ready immediately after upgrade succeeds;
  server version available via explicit `version` request
- **`notifications_only` in hello** — moves to WS upgrade query param:
  `/ws?notifications_only=true`

## Error Handling & Edge Cases

- **JWKS fetch failure at startup:** Fatal. Server won't start if it can't
  reach Passport.
- **JWKS refresh failure at runtime:** Log error, keep using cached keys.
  Requests continue with stale keys until next successful refresh.
- **Expired JWT mid-WS connection:** No impact. JWT validated once at upgrade.
  Connection lives until client disconnects.
- **Invalid token on `/ws` upgrade:** HTTP 401 before upgrade completes. No
  WebSocket connection established.
- **Invalid token on `/mcp` request:** JSON-RPC error with code -32001.
- **API key cache miss:** Synchronous `POST /v1/verify-api-key`. Bridge
  requests block until verified.
- **Auto-provisioning race:** `INSERT OR IGNORE` — two concurrent
  first-connections for the same Passport user are safe.

## End-to-End Auth Strategy

Three client contexts, each with a different auth path:

**Browser (future Sharkfin frontend in Scope shell):**
```
Browser → Scope BFF proxy → Sharkfin
         (cookie → JWT)      (validates JWT)
```
The BFF intercepts requests to `/api/sharkfin/*`, converts the session cookie
to a JWT via Passport's `/v1/token` endpoint, and forwards with
`Authorization: Bearer`. For WebSocket, the BFF does the same at upgrade time.
The TS client in-browser connects to a relative URL (`/api/sharkfin/ws`) with
no auth config — the BFF handles it transparently.

**Node.js / CLI (direct connection):**
```
Node process → Sharkfin directly
               (Bearer token or API key)
```
The TS client accepts optional `token` or `apiKey` in connect options.
Attached as HTTP headers during WS upgrade.

**Go client (direct connection):**
Same pattern: `WithToken(string)` or `WithAPIKey(string)` dial options.

**Client library changes:**
- Remove: `Register()`, `Identify()`, hello handshake
- Add: `token` / `apiKey` options at connect time (HTTP headers on upgrade)
- Connection ready immediately after upgrade succeeds
- Server version via explicit `Version()` request (already exists)

The TS client stays auth-agnostic — it doesn't know about cookies or Passport.
This means the future frontend uses the TS client as-is with no auth wiring.

## Testing

- **Unit tests:** Mock Passport validators (implement `auth.Validator`
  interface returning canned identities). Test middleware rejects invalid
  tokens, accepts valid. Test auto-provisioning creates identity. Test
  `notifications_only` query param parsing.
- **E2E tests:** Minimal JWKS stub server alongside the daemon. Test full WS
  connect with JWT, MCP request with Bearer, API key auth for bridge.
- **Client library tests:** Update mock servers to skip hello handshake. Test
  token/apiKey header attachment on upgrade.
