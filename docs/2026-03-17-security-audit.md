# Cross-Repo Security Audit — 2026-03-17

Audited: Passport, Scope BFF, Sharkfin

---

## Critical

| # | Repo | File | Issue |
|---|---|---|---|
| 1 | Sharkfin | `ws_handler.go:497` | WS `history` bypasses channel membership check — any authenticated user can read any channel |
| 2 | Sharkfin | `ws_handler.go:629` | WS `dm_list` returns all DMs (uses admin view instead of user-scoped) |
| 3 | Passport | `auth.ts:67` | Missing `BETTER_AUTH_SECRET` startup validation — undefined secret accepted silently |
| 4 | Passport | `app.ts:49-54` | Sign-up guard allows any authenticated session, not just admin role |
| 5 | Scope | `ws.go:12-14` | WebSocket `CheckOrigin` returns `true` — cross-site WS hijacking possible |
| 6 | Scope | `cmd/web/web.go:73` | No HTTP server timeouts or body size limits — DoS via slow connections or large bodies |

## High

| # | Repo | File | Issue |
|---|---|---|---|
| 7 | Passport | `verify-api-key.ts:32` | Unauthenticated API key verification endpoint — brute-forceable |
| 8 | Passport | (all) | No rate limiting on sign-in/sign-up endpoints |
| 9 | Passport | `verify-api-key.ts:76` | Raw error objects logged — potential internal detail leakage |

## Important

| # | Repo | File | Issue |
|---|---|---|---|
| 10 | Scope | `proxy.go:72-83` | Session cookie missing enforced HttpOnly/Secure/SameSite attributes |
| 11 | Scope | `ws.go:54-56` | WS auth relies on header side-effect — brittle if call path changes |
| 12 | Scope | `bff.go:37` | Token cache unbounded growth — no TTL eviction, memory exhaustion in multi-user |
| 13 | Scope | `handler.go:152-156` | `isUIAssetRequest` too broad — should whitelist specific sub-paths |
| 14 | Sharkfin | `ws_handler.go:17` | WebSocket upgrader accepts any origin (same as Scope issue 5) |
| 15 | Sharkfin | `identities.go:101` | `WipeAll` concatenates table names into SQL (hardcoded but fragile pattern) |

## Medium

| # | Repo | File | Issue |
|---|---|---|---|
| 16 | Passport | `app.ts:43-58` | Sign-up guard falls through on DB error — allows unauthenticated sign-up window |
| 17 | Passport | `auth.ts:66` | Default `http://` base URL disables Secure cookie flag in production |
| 18 | Passport | `seed.ts:95` | API keys logged to stdout in plaintext during seed |
| 19 | Passport | `test/global-setup.ts` | Static weak test secrets in source control |

## Verified Clean

- **SQL injection (Sharkfin):** All SQLite queries use parameterized `?` placeholders
- **XSS (Sharkfin web UI):** SolidJS renders user content as text nodes, not innerHTML
- **Token handling (Sharkfin web UI):** No tokens in localStorage/sessionStorage — BFF handles auth
- **Hub race conditions (Sharkfin):** Three-phase broadcast pattern is correct
- **MCP authorization (Sharkfin):** Permission checks cover all privileged tools
- **Path traversal (Scope):** SPA handler uses embed.FS which rejects `..`
- **SSRF (Scope):** Service URLs from local config — user attacking themselves
- **Token leakage in logs (Scope):** No JWT/cookie values logged
- **CORS (Passport):** No CORS headers set — correct since browsers go through BFF
- **Password hashing (Passport):** Uses Argon2 via @node-rs/argon2

---

## Priority Fix Order

**Immediate (before next deployment):**
1. Sharkfin #1 — Add `IsChannelMember` check to WS `history` handler
2. Sharkfin #2 — Replace `ListAllDMs()` with `ListDMsForUser(identityID)` in WS `dm_list`
3. Passport #3 — Add startup guard: exit if `BETTER_AUTH_SECRET` is unset
4. Passport #4 — Check `session.user.role === "admin"` in sign-up guard

**Before production:**
5. Scope #5 + Sharkfin #14 — Fix WebSocket origin validation in both repos
6. Scope #6 — Add server timeouts and MaxHeaderBytes
7. Passport #8 — Add rate limiting to sign-in/sign-up
8. Passport #16 — Wrap sign-up guard DB query in try/catch, return 503 on error
