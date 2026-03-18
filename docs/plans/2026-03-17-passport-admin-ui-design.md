# Passport Admin UI Design — 2026-03-17

## Problem

A fresh Passport + Shell deployment has no way to create service keys or agent keys through the browser. The only path is pre-seeded keys (via Passport's seed script) or manual database manipulation. This creates a chicken-and-egg problem: services like Sharkfin need API keys to authenticate with Passport, but there's no UI to create them.

## Solution

Build a Passport admin UI as a React Module Federation remote, loaded by the shell like any other service. The admin UI provides CRUD for three credential types: users, service keys, and agent keys.

## Identity Types

Passport manages three distinct identity types:

| Type | Purpose | Authentication | Example |
|------|---------|---------------|---------|
| `user` | Human accounts | Email/password → session cookie | `admin@workfort.dev` |
| `service` | Daemon/server identity | API key in server config | Sharkfin daemon |
| `agent` | AI agent identity | API key in agent config (e.g. `.mcp.json`) | Claude Code MCP bridge |

These types are enforced at creation time in Passport and checked by downstream services (e.g. Sharkfin auth middleware uses the type to determine access level).

## Architecture

```
Browser → BFF (/forts/{fort}/api/auth/v1/...) → Passport
                                                  ├── Better Auth admin endpoints (user CRUD)
                                                  └── Custom Hono routes (API key CRUD)
```

- **Approach:** Hybrid — use Better Auth's existing admin plugin endpoints for user management, custom Hono routes for API key management where the plugin's endpoints are insufficient.
- **All requests go through the BFF** — the React UI never calls Passport directly.
- **Admin-only access** — the entire Passport UI is only visible to users with Passport role `admin`.

## Backend Changes (Passport)

### Existing Better Auth Admin Endpoints (no changes needed)

- `GET /v1/admin/list-users` — list users with search/filter/sort/pagination
- `POST /v1/admin/create-user` — create user (with type, role fields)
- `POST /v1/admin/update-user` — update user fields
- `POST /v1/admin/remove-user` — delete user
- `POST /v1/admin/set-role` — change user role
- `POST /v1/admin/ban-user` — deactivate user (UI terminology: "inactive")
- `POST /v1/admin/unban-user` — reactivate user

### Custom Routes Needed

- `GET /v1/admin/api-keys` — list all API keys across all users (Better Auth's `/v1/api-key/list` only returns keys for the authenticated user)
- `POST /v1/api-key/create` — already exists, needs type enforcement
- `POST /v1/api-key/delete` — already exists
- `POST /v1/api-key/update` — already exists

### New Behavior

1. **Last-admin guard** — before `remove-user` and `set-role`, check if the operation would leave zero admin users. Return error if so.
2. **`/ui/health` update** — return `route: "/admin"` and `admin_only: true`:
   ```json
   {
     "status": "ok",
     "name": "auth",
     "label": "Admin",
     "route": "/admin",
     "admin_only": true
   }
   ```
3. **Static UI serving** — serve `web/dist/` at `/ui/*` for MF remote assets (same pattern as Sharkfin daemon).
4. **Identity type enforcement** — API key creation must specify `type` (`service` or `agent`). Type is stored in key metadata and returned by `/v1/verify-api-key`.

## BFF Changes (Scope)

1. **Admin-only service filtering** — `GET /api/services` omits services with `admin_only: true` unless the requesting user's session has `role: "admin"`. Non-admin users see no trace of admin services.
2. **Session role passthrough** — `GET /api/session` includes the user's role from Passport's session response.

## Sharkfin Changes

1. **Identity type checking** — auth middleware reads the identity `type` from Passport's verification response and maps it to the appropriate Sharkfin role:
   - `service` → service-level access
   - `agent` → agent role
   - `user` → user role

## Frontend (Passport React MF Remote)

### Stack

- React + Vite + Module Federation
- `@workfort/ui` web components (Lit, framework-agnostic)
- `@workfort/ui-react` hooks

### Pages (4 total)

1. **Users** — table of all user-type identities
   - Columns: username, email, display name, role, status (active/inactive)
   - Actions: create, edit role, deactivate/reactivate, delete
   - Create modal: email, username, display name, password, role
   - Last-admin guard: delete/demote blocked with error banner if last admin

2. **Service Keys** — table of service-type API keys
   - Columns: name, prefix (masked), created date
   - Actions: create, revoke/delete
   - Create modal: name. Raw key shown once in dialog with copy button.

3. **Agent Keys** — table of agent-type API keys
   - Columns: name, prefix (masked), created date
   - Actions: create, revoke/delete
   - Create modal: name. Raw key shown once in dialog with copy button.

4. **Overview** (v2, not in initial implementation)

### Navigation

Tab bar within the MF remote: Users | Service Keys | Agent Keys

### Terminology

- "Inactive" (not "banned") for deactivated users. "Banned" is reserved for future punitive/irrecoverable state.
- "Revoke" for deleting API keys.

### Component Reuse

- `wf-list`, `wf-list-item` for tables
- `wf-dialog` for create/edit modals and key display
- `wf-banner` for errors and confirmations
- `wf-button` for actions
- `wf-skeleton` for loading states

## Bootstrap Flow

Fresh deployment, end to end:

1. Passport starts — no users, `/ui/health` returns `setup_mode: true`
2. Shell shows SetupForm — admin creates their account (already implemented)
3. Admin signs in — shell loads, nav shows "Admin" (Passport MF remote)
4. Admin navigates to Admin → Service Keys — creates a key for Sharkfin, copies raw value
5. Admin configures Sharkfin daemon — adds service key to `~/.config/sharkfin/config.yaml`
6. Admin creates agent key — for MCP bridge or other AI agents
7. Sharkfin daemon starts, authenticates with Passport via service key
8. Agent connects to Sharkfin with agent key, gets provisioned with agent role

## Security Considerations

- Admin UI visibility is server-side gated — BFF never sends admin-only services to non-admin sessions
- Last-admin invariant enforced server-side in Passport, not just UI
- API key raw value shown exactly once at creation, never retrievable after
- Identity types enforced at creation and verified by downstream services
- Future: gateway becomes the trust boundary for service exposure (currently BFF reads from local config)

## Out of Scope

- User self-service profile management (future)
- Banned user state (future, distinct from inactive)
- Service-level role management in Passport (Sharkfin manages its own RBAC)
- Gateway trust boundary (not yet built)
