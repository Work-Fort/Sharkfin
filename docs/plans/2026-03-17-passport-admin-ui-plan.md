# Passport Admin UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a React Module Federation remote in Passport that provides admin CRUD for users, service keys, and agent keys, loaded by the shell like any other service.

**Architecture:** Passport gets a `web/` directory with a React + Vite MF remote. The shell's service-mount is extended to support framework-agnostic remotes (mount/unmount functions instead of SolidJS components). The BFF filters admin-only services from non-admin sessions. Passport's existing Better Auth admin endpoints handle user CRUD; custom Hono routes handle API key listing across users.

**Tech Stack:** React 18, Vite, @module-federation/vite, @workfort/ui (Lit web components), @workfort/ui-react (hooks), Hono (backend routes), Better Auth admin + API key plugins

**Design Doc:** `docs/plans/2026-03-17-passport-admin-ui-design.md`

---

## Task 1: Framework-Agnostic Service Mount (Shell)

The shell currently uses SolidJS `<Dynamic>` to render MF remotes. This only works for SolidJS components. We need to support remotes that export `mount(el, props)` / `unmount(el)` functions for non-Solid frameworks (React, Vue, Svelte).

**Files:**
- Modify: `/home/kazw/Work/WorkFort/scope/lead/web/shell/src/lib/remotes.ts` — update `ServiceModule` type
- Modify: `/home/kazw/Work/WorkFort/scope/lead/web/shell/src/components/service-mount.tsx` — add imperative mount path

**Step 1: Update ServiceModule type in remotes.ts**

Add optional `mount`/`unmount` to the interface:

```typescript
export interface ServiceModule {
  // SolidJS remotes: export default component
  default?: (props: { connected: boolean }) => any;
  // Framework-agnostic remotes: export mount/unmount
  mount?: (el: HTMLElement, props: { connected: boolean }) => void;
  unmount?: (el: HTMLElement) => void;
  manifest: { name: string; label: string; route: string; minWidth?: number };
  SidebarContent?: () => any;
  HeaderActions?: () => any;
}
```

Update `loadServiceModule` validation to accept either `default` or `mount`:

```typescript
export async function loadServiceModule(serviceName: string): Promise<ServiceModule> {
  const mod = await loadRemote<ServiceModule>(`${serviceName}/index`);
  if (!mod || (!mod.default && !mod.mount) || !mod.manifest) {
    throw new Error(
      `Remote "${serviceName}" must export (default OR mount) and manifest`,
    );
  }
  return mod;
}
```

**Step 2: Add imperative mount path in service-mount.tsx**

Replace the simple `<Dynamic>` with a branching strategy:

```typescript
import { createResource, Suspense, ErrorBoundary, Show, onCleanup, type Component } from 'solid-js';
import { Dynamic } from 'solid-js/web';
import { loadServiceModule, type ServiceModule } from '../lib/remotes';
import Unavailable from './unavailable';

/** Imperative mount for non-Solid remotes (React, Vue, etc.) */
function ImperativeMount(props: { mod: ServiceModule; connected: boolean }) {
  let el!: HTMLDivElement;

  const mounted = () => {
    props.mod.mount!(el, { connected: props.connected });
  };

  onCleanup(() => {
    if (props.mod.unmount) props.mod.unmount(el);
  });

  return <div ref={(r) => { el = r; queueMicrotask(mounted); }} style="display:contents" />;
}

const ServiceMount: Component<{
  name: string;
  label: string;
  connected: boolean;
  onModule?: (mod: ServiceModule | null) => void;
}> = (props) => {
  const [mod] = createResource(
    () => props.name,
    async (name) => {
      const m = await loadServiceModule(name);
      props.onModule?.(m);
      return m;
    },
  );

  return (
    <ErrorBoundary fallback={<Unavailable label={props.label} />}>
      <Suspense fallback={<wf-skeleton width="100%" height="200px" />}>
        <Show
          when={mod() || props.connected}
          fallback={
            <wf-banner
              variant="warning"
              headline={`${props.label} is starting up or temporarily unavailable. This page will update automatically when it's ready.`}
            />
          }
        >
          <Show when={mod()}>
            {(m) => (
              m().mount
                ? <ImperativeMount mod={m()} connected={props.connected} />
                : <Dynamic component={m().default!} connected={props.connected} />
            )}
          </Show>
        </Show>
      </Suspense>
    </ErrorBoundary>
  );
};

export default ServiceMount;
```

**Step 3: Run shell dev server and verify Sharkfin still loads**

```bash
cd ~/Work/WorkFort/scope/lead/web/shell && pnpm dev
```

Navigate to `/forts/local/chat` — Sharkfin should still load via the `default` export path.

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/shell/src/lib/remotes.ts web/shell/src/components/service-mount.tsx
git commit -m "feat(shell): support framework-agnostic MF remotes via mount/unmount"
```

---

## Task 2: Admin-Only Service Filtering (BFF + Shell)

Services with `admin_only: true` in their health manifest should only be visible to admin users. The BFF filters server-side; the shell never receives admin-only services for non-admin sessions.

**Files:**
- Modify: `/home/kazw/Work/WorkFort/scope/lead/internal/infra/httpapi/tracker.go` — add `AdminOnly` field to `TrackedService`
- Modify: `/home/kazw/Work/WorkFort/scope/lead/internal/infra/httpapi/handler.go` — filter admin-only services in `servicesHandler`, add role to session response
- Modify: `/home/kazw/Work/WorkFort/scope/lead/web/shell/src/lib/api.ts` — add `admin_only` to `ServiceInfo` type
- Modify: `/home/kazw/Work/WorkFort/scope/lead/web/shell/src/stores/services.ts` — no filtering needed (BFF handles it)

**Step 1: Add AdminOnly to TrackedService struct**

In `tracker.go`, add field to `TrackedService`:

```go
type TrackedService struct {
	URL       string   `json:"-"`
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Route     string   `json:"route"`
	Enabled   bool     `json:"enabled"`
	UI        bool     `json:"ui"`
	Connected bool     `json:"connected"`
	SetupMode bool     `json:"setup_mode,omitempty"`
	AdminOnly bool     `json:"admin_only,omitempty"`
	WSPaths   []string `json:"-"`
}
```

Update `probeOne` to parse `admin_only` from the health response manifest. Find where the manifest is decoded and add:

```go
type healthManifest struct {
	Status    string   `json:"status"`
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Route     string   `json:"route"`
	SetupMode bool     `json:"setup_mode"`
	AdminOnly bool     `json:"admin_only"`
	WSPaths   []string `json:"ws_paths"`
}
```

And set `svc.AdminOnly = manifest.AdminOnly` when updating the tracked service.

**Step 2: Update session handler to return user role**

In `handler.go`, update `sessionHandler` to return the role from the JWT claims:

```go
func sessionHandler(tc *TokenConverter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"authenticated": false}
		if tc != nil {
			tok, err := tc.Token(r)
			if err == nil {
				resp["authenticated"] = true
				// Extract role from JWT claims
				if claims, ok := tok.Claims.(jwt.MapClaims); ok {
					if role, ok := claims["role"].(string); ok {
						resp["role"] = role
					}
				}
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}
```

Note: Check how the BFF parses JWT claims — the exact type depends on the JWT library used. Adapt accordingly.

**Step 3: Filter admin-only services in servicesHandler**

In `handler.go`, update `servicesHandler` to accept the token converter and filter based on role:

```go
func servicesHandler(fortName string, tracker *ServiceTracker, tc *TokenConverter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		isAdmin := false
		if tc != nil {
			tok, err := tc.Token(r)
			if err == nil {
				// Extract role from JWT claims — adapt to actual JWT library
				if claims, ok := tok.Claims.(jwt.MapClaims); ok {
					isAdmin = claims["role"] == "admin"
				}
			}
		}

		all := tracker.Services()
		var visible []TrackedService
		for _, svc := range all {
			if svc.AdminOnly && !isAdmin {
				continue
			}
			visible = append(visible, svc)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"fort":      fortName,
			"services":  visible,
			"conflicts": tracker.Conflicts(),
		})
	}
}
```

**Step 4: Update shell TypeScript types**

In `api.ts`, add `admin_only` to `ServiceInfo`:

```typescript
export interface ServiceInfo {
  name: string;
  label: string;
  route: string;
  enabled: boolean;
  ui: boolean;
  connected: boolean;
  setup_mode?: boolean;
  admin_only?: boolean;
}
```

Update session response type to include role:

```typescript
export interface SessionResponse {
  authenticated: boolean;
  role?: string;
}
```

**Step 5: Build and verify**

```bash
cd ~/Work/WorkFort/scope/lead && mise run build
```

**Step 6: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add internal/infra/httpapi/tracker.go internal/infra/httpapi/handler.go web/shell/src/lib/api.ts
git commit -m "feat(bff): filter admin-only services from non-admin sessions"
```

---

## Task 3: Passport /ui/health and Static Asset Serving

Update Passport to serve its admin UI as a Module Federation remote, discoverable by the BFF.

**Files:**
- Modify: `/home/kazw/Work/WorkFort/passport/lead/src/app.ts` — update `/ui/health` response, add static file serving
- Modify: `/home/kazw/Work/WorkFort/passport/lead/src/index.ts` — wire up static serving

**Step 1: Update /ui/health response**

In `app.ts`, change the `/ui/health` handler:

```typescript
app.get("/ui/health", async (c) => {
  let setupMode = false;
  try {
    const ctx = await (auth as any).$context;
    const rows = await ctx.adapter.findMany({ model: "user", limit: 1 });
    setupMode = !rows || rows.length === 0;
  } catch {
    setupMode = true;
  }

  const body: Record<string, unknown> = {
    status: "ok",
    name: "auth",
    label: "Admin",
    route: "/admin",
    admin_only: true,
  };

  if (setupMode) {
    body.setup_mode = true;
  }

  // Return 200 when UI assets exist, 503 otherwise
  // For now, always 200 since we're building the UI
  return c.json(body, 200);
});
```

**Step 2: Add static file serving for /ui/***

In `app.ts`, add a route to serve the built web assets:

```typescript
import { serveStatic } from '@hono/node-server/serve-static';

// Serve MF remote assets at /ui/*
app.use('/ui/*', serveStatic({
  root: './web/dist',
  rewriteRequestPath: (path) => path.replace('/ui', ''),
}));
```

Note: Check if `@hono/node-server` has `serveStatic`. If not, use Node's `fs` to serve files, or add the dependency.

**Step 3: Verify health endpoint**

```bash
curl -s http://passport.nexus:3000/ui/health | jq .
```

Expected:
```json
{
  "status": "ok",
  "name": "auth",
  "label": "Admin",
  "route": "/admin",
  "admin_only": true
}
```

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add src/app.ts
git commit -m "feat: update /ui/health for admin UI discovery and add static serving"
```

---

## Task 4: Last-Admin Guard (Passport)

Prevent deletion or demotion of the last admin user.

**Files:**
- Modify: `/home/kazw/Work/WorkFort/passport/lead/src/app.ts` — add middleware guards on admin endpoints
- Create: `/home/kazw/Work/WorkFort/passport/lead/src/test/last-admin-guard.test.ts`

**Step 1: Write failing test**

```typescript
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';

const port = readFileSync('data/.test-port', 'utf8').trim();
const BASE = `http://127.0.0.1:${port}`;

describe('last-admin guard', () => {
  it('rejects removing the last admin', async () => {
    // Sign in as admin
    const signIn = await fetch(`${BASE}/v1/sign-in/email`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'admin@workfort.dev', password: 'adminpass123!' }),
    });
    const cookie = signIn.headers.get('set-cookie')!;

    // Get admin user ID
    const listRes = await fetch(`${BASE}/v1/admin/list-users`, {
      headers: { cookie },
    });
    const users = await listRes.json();
    const admin = users.users.find((u: any) => u.role === 'admin');

    // Try to remove the only admin
    const removeRes = await fetch(`${BASE}/v1/admin/remove-user`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie },
      body: JSON.stringify({ userId: admin.id }),
    });

    expect(removeRes.status).toBe(400);
    const body = await removeRes.json();
    expect(body.error).toContain('last admin');
  });

  it('rejects demoting the last admin', async () => {
    const signIn = await fetch(`${BASE}/v1/sign-in/email`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'admin@workfort.dev', password: 'adminpass123!' }),
    });
    const cookie = signIn.headers.get('set-cookie')!;

    const listRes = await fetch(`${BASE}/v1/admin/list-users`, {
      headers: { cookie },
    });
    const users = await listRes.json();
    const admin = users.users.find((u: any) => u.role === 'admin');

    // Try to demote the only admin to user
    const setRoleRes = await fetch(`${BASE}/v1/admin/set-role`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie },
      body: JSON.stringify({ userId: admin.id, role: 'user' }),
    });

    expect(setRoleRes.status).toBe(400);
    const body = await setRoleRes.json();
    expect(body.error).toContain('last admin');
  });
});
```

**Step 2: Run test to verify it fails**

```bash
cd ~/Work/WorkFort/passport/lead && pnpm vitest run src/test/last-admin-guard.test.ts
```

Expected: FAIL (no guard implemented yet, operations succeed)

**Step 3: Implement guard middleware**

In `app.ts`, add guards before the Better Auth catch-all:

```typescript
// Last-admin guard: prevent removing or demoting the last admin
async function countAdmins(): Promise<number> {
  const ctx = await (auth as any).$context;
  const admins = await ctx.adapter.findMany({
    model: "user",
    where: [{ field: "role", value: "admin" }],
  });
  return admins?.length ?? 0;
}

app.post("/v1/admin/remove-user", async (c, next) => {
  const body = await c.req.json();
  const ctx = await (auth as any).$context;
  const user = await ctx.adapter.findOne({ model: "user", where: [{ field: "id", value: body.userId }] });
  if (user?.role === "admin") {
    const count = await countAdmins();
    if (count <= 1) {
      return c.json({ error: "Cannot remove the last admin" }, 400);
    }
  }
  return next();
});

app.post("/v1/admin/set-role", async (c, next) => {
  const body = await c.req.json();
  if (body.role !== "admin") {
    const ctx = await (auth as any).$context;
    const user = await ctx.adapter.findOne({ model: "user", where: [{ field: "id", value: body.userId }] });
    if (user?.role === "admin") {
      const count = await countAdmins();
      if (count <= 1) {
        return c.json({ error: "Cannot demote the last admin" }, 400);
      }
    }
  }
  return next();
});
```

Note: The exact adapter query syntax may differ — check Better Auth's adapter API. The guard must be registered BEFORE the catch-all `app.all("/v1/*", ...)` route.

**Step 4: Run test to verify it passes**

```bash
cd ~/Work/WorkFort/passport/lead && pnpm vitest run src/test/last-admin-guard.test.ts
```

Expected: PASS

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add src/app.ts src/test/last-admin-guard.test.ts
git commit -m "feat: add last-admin guard for user removal and role demotion"
```

---

## Task 5: Custom Admin API Key Routes (Passport)

Better Auth's `/v1/api-key/list` only returns keys for the authenticated user. Admin needs to list all keys across all users. Also need to enforce identity type on creation.

**Files:**
- Modify: `/home/kazw/Work/WorkFort/passport/lead/src/app.ts` — add custom admin API key routes
- Create: `/home/kazw/Work/WorkFort/passport/lead/src/test/admin-api-keys.test.ts`

**Step 1: Write failing test**

```typescript
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';

const port = readFileSync('data/.test-port', 'utf8').trim();
const BASE = `http://127.0.0.1:${port}`;

async function adminCookie(): Promise<string> {
  const res = await fetch(`${BASE}/v1/sign-in/email`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email: 'admin@workfort.dev', password: 'adminpass123!' }),
  });
  return res.headers.get('set-cookie')!;
}

describe('admin API key management', () => {
  it('lists all API keys across users', async () => {
    const cookie = await adminCookie();
    const res = await fetch(`${BASE}/v1/admin/api-keys`, { headers: { cookie } });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(Array.isArray(body.keys)).toBe(true);
  });

  it('creates a service key with type enforcement', async () => {
    const cookie = await adminCookie();

    // Create a service user first
    const createUser = await fetch(`${BASE}/v1/admin/create-user`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie },
      body: JSON.stringify({
        email: 'svc-test@workfort.dev',
        password: 'testpass123!',
        name: 'Test Service',
        role: 'user',
        data: { username: 'svc-test', displayName: 'Test Service', type: 'service' },
      }),
    });
    const user = await createUser.json();

    // Create API key with type
    const createKey = await fetch(`${BASE}/v1/api-key/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie },
      body: JSON.stringify({
        userId: user.id,
        prefix: 'wf-svc',
        name: 'test-service-key',
        metadata: { type: 'service' },
      }),
    });
    expect(createKey.status).toBe(200);
    const key = await createKey.json();
    expect(key.key).toBeTruthy(); // Raw key shown once
  });
});
```

**Step 2: Run test to verify it fails**

```bash
cd ~/Work/WorkFort/passport/lead && pnpm vitest run src/test/admin-api-keys.test.ts
```

Expected: FAIL (404 on `/v1/admin/api-keys`)

**Step 3: Implement admin API key listing route**

In `app.ts`, add before the catch-all:

```typescript
// Admin: list all API keys across all users
app.get("/v1/admin/api-keys", async (c) => {
  // Verify admin session
  const session = await auth.api.getSession({ headers: c.req.raw.headers }).catch(() => null);
  if (!session || (session as any).user?.role !== "admin") {
    return c.json({ error: "Admin access required" }, 403);
  }

  const ctx = await (auth as any).$context;
  const keys = await ctx.adapter.findMany({ model: "apikey" });

  // Don't return hashed keys — only metadata, prefix, dates
  const sanitized = (keys || []).map((k: any) => ({
    id: k.id,
    name: k.name,
    prefix: k.prefix,
    userId: k.userId,
    metadata: k.metadata,
    createdAt: k.createdAt,
    expiresAt: k.expiresAt,
    enabled: k.enabled,
  }));

  return c.json({ keys: sanitized });
});
```

Note: The exact adapter model name for API keys may differ — check Better Auth's schema. Could be `"apikey"`, `"apiKey"`, or `"api_key"`.

**Step 4: Run test to verify it passes**

```bash
cd ~/Work/WorkFort/passport/lead && pnpm vitest run src/test/admin-api-keys.test.ts
```

Expected: PASS

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add src/app.ts src/test/admin-api-keys.test.ts
git commit -m "feat: add admin endpoint for listing all API keys"
```

---

## Task 6: Passport React MF Remote Scaffold

Create the React MF remote project structure in Passport.

**Files:**
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/package.json`
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/vite.config.ts`
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/tsconfig.json`
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/index.tsx` — MF entry point with mount/unmount
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/App.tsx` — root React component
- Modify: `/home/kazw/Work/WorkFort/passport/lead/pnpm-workspace.yaml` — add web workspace

**Step 1: Create package.json**

```json
{
  "name": "@workfort/passport-admin-ui",
  "version": "0.0.1",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build"
  },
  "dependencies": {
    "react": "^18.3.0",
    "react-dom": "^18.3.0",
    "@workfort/ui": "link:/home/kazw/Work/WorkFort/scope/lead/web/packages/ui",
    "@workfort/ui-react": "link:/home/kazw/Work/WorkFort/scope/lead/web/packages/react"
  },
  "devDependencies": {
    "@module-federation/vite": "^1.1.0",
    "@types/react": "^18.3.0",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.0",
    "typescript": "^5.6.0",
    "vite": "^6.0.0"
  }
}
```

**Step 2: Create vite.config.ts**

```typescript
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { federation } from '@module-federation/vite';

export default defineConfig({
  plugins: [
    react(),
    federation({
      name: 'auth',
      filename: 'remoteEntry.js',
      exposes: {
        './index': './src/index.tsx',
      },
      shared: {
        react: { singleton: true },
        'react-dom': { singleton: true },
        '@workfort/ui': { singleton: true },
        '@workfort/ui-react': { singleton: true },
      },
    }),
  ],
  build: {
    target: 'esnext',
    outDir: 'dist',
  },
});
```

**Step 3: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "outDir": "dist",
    "types": ["vite/client"]
  },
  "include": ["src"]
}
```

**Step 4: Create src/index.tsx — MF entry with mount/unmount**

This is the critical file. The shell expects either a SolidJS `default` export OR `mount`/`unmount` functions. Since this is React, we use the imperative mount pattern.

```tsx
import React from 'react';
import { createRoot, Root } from 'react-dom/client';
import App from './App';

// Store roots for cleanup
const roots = new WeakMap<HTMLElement, Root>();

export function mount(el: HTMLElement, props: { connected: boolean }) {
  let root = roots.get(el);
  if (!root) {
    root = createRoot(el);
    roots.set(el, root);
  }
  root.render(<App connected={props.connected} />);
}

export function unmount(el: HTMLElement) {
  const root = roots.get(el);
  if (root) {
    root.unmount();
    roots.delete(el);
  }
}

export const manifest = {
  name: 'auth',
  label: 'Admin',
  route: '/admin',
};
```

**Step 5: Create src/App.tsx — placeholder root component**

```tsx
import React from 'react';
import '@workfort/ui/styles';

export default function App({ connected }: { connected: boolean }) {
  return (
    <div style={{ padding: 'var(--wf-space-lg)' }}>
      <h1>Admin</h1>
      <p>Users, Service Keys, Agent Keys — coming next.</p>
    </div>
  );
}
```

**Step 6: Update pnpm-workspace.yaml**

Check current workspace config and add `web`:

```yaml
packages:
  - "packages/*"
  - "web"
```

**Step 7: Install dependencies and build**

```bash
cd ~/Work/WorkFort/passport/lead/web && pnpm install && pnpm build
```

**Step 8: Verify remoteEntry.js exists**

```bash
ls ~/Work/WorkFort/passport/lead/web/dist/remoteEntry.js
```

**Step 9: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add web/ pnpm-workspace.yaml pnpm-lock.yaml
git commit -m "feat: scaffold React MF remote for admin UI"
```

---

## Task 7: Users Page

Build the Users CRUD page.

**Files:**
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/pages/Users.tsx`
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/components/CreateUserDialog.tsx`
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/lib/api.ts` — API client
- Modify: `/home/kazw/Work/WorkFort/passport/lead/web/src/App.tsx` — add routing/tabs

**Step 1: Create API client**

```typescript
// web/src/lib/api.ts

// Fort prefix is extracted from the current URL: /forts/{fort}/admin/...
function fortPrefix(): string {
  const match = window.location.pathname.match(/^\/forts\/([^/]+)/);
  return match ? `/forts/${match[1]}/api/auth` : '/api/auth';
}

async function apiFetch(path: string, opts?: RequestInit): Promise<Response> {
  return fetch(`${fortPrefix()}${path}`, {
    ...opts,
    headers: { 'Content-Type': 'application/json', ...opts?.headers },
    credentials: 'include',
  });
}

export interface User {
  id: string;
  email: string;
  name: string;
  username?: string;
  displayName?: string;
  role?: string;
  type?: string;
  banned?: boolean;
  createdAt: string;
}

export async function listUsers(): Promise<User[]> {
  const res = await apiFetch('/v1/admin/list-users');
  const data = await res.json();
  return data.users ?? [];
}

export async function createUser(body: {
  email: string;
  password: string;
  name: string;
  role: string;
  data: { username: string; displayName: string; type: string };
}): Promise<User> {
  const res = await apiFetch('/v1/admin/create-user', {
    method: 'POST',
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to create user');
  return res.json();
}

export async function removeUser(userId: string): Promise<void> {
  const res = await apiFetch('/v1/admin/remove-user', {
    method: 'POST',
    body: JSON.stringify({ userId }),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to remove user');
}

export async function setRole(userId: string, role: string): Promise<void> {
  const res = await apiFetch('/v1/admin/set-role', {
    method: 'POST',
    body: JSON.stringify({ userId, role }),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to set role');
}

export async function deactivateUser(userId: string): Promise<void> {
  const res = await apiFetch('/v1/admin/ban-user', {
    method: 'POST',
    body: JSON.stringify({ userId }),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to deactivate user');
}

export async function reactivateUser(userId: string): Promise<void> {
  const res = await apiFetch('/v1/admin/unban-user', {
    method: 'POST',
    body: JSON.stringify({ userId }),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to reactivate user');
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  userId: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
  expiresAt?: string;
  enabled?: boolean;
}

export async function listAllApiKeys(): Promise<ApiKey[]> {
  const res = await apiFetch('/v1/admin/api-keys');
  const data = await res.json();
  return data.keys ?? [];
}

export async function createApiKey(body: {
  userId: string;
  prefix: string;
  name: string;
  metadata: Record<string, unknown>;
}): Promise<{ key: string; id: string }> {
  const res = await apiFetch('/v1/api-key/create', {
    method: 'POST',
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to create key');
  return res.json();
}

export async function deleteApiKey(keyId: string): Promise<void> {
  const res = await apiFetch('/v1/api-key/delete', {
    method: 'POST',
    body: JSON.stringify({ keyId }),
  });
  if (!res.ok) throw new Error((await res.json()).error ?? 'Failed to revoke key');
}
```

**Step 2: Create Users page**

```tsx
// web/src/pages/Users.tsx
import React, { useEffect, useState } from 'react';
import { listUsers, removeUser, setRole, deactivateUser, reactivateUser, type User } from '../lib/api';
import CreateUserDialog from '../components/CreateUserDialog';

export default function Users() {
  const [users, setUsers] = useState<User[]>([]);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);

  const refresh = async () => {
    try {
      setUsers(await listUsers());
      setError('');
    } catch (e: any) {
      setError(e.message);
    }
  };

  useEffect(() => { refresh(); }, []);

  const handleDelete = async (user: User) => {
    if (!confirm(`Delete user ${user.username ?? user.email}?`)) return;
    try {
      await removeUser(user.id);
      await refresh();
    } catch (e: any) {
      setError(e.message);
    }
  };

  const handleToggleActive = async (user: User) => {
    try {
      if (user.banned) {
        await reactivateUser(user.id);
      } else {
        await deactivateUser(user.id);
      }
      await refresh();
    } catch (e: any) {
      setError(e.message);
    }
  };

  const handleRoleChange = async (user: User, newRole: string) => {
    try {
      await setRole(user.id, newRole);
      await refresh();
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--wf-space-md)' }}>
        <h2 style={{ margin: 0 }}>Users</h2>
        <wf-button variant="primary" onClick={() => setShowCreate(true)}>Create User</wf-button>
      </div>

      {error && <wf-banner variant="error" headline={error} />}

      <wf-list>
        {users.map((u) => (
          <wf-list-item key={u.id}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
              <div>
                <strong>{u.username ?? u.email}</strong>
                {u.displayName && <span style={{ color: 'var(--wf-color-text-secondary)', marginLeft: 'var(--wf-space-sm)' }}>{u.displayName}</span>}
                <div style={{ fontSize: 'var(--wf-text-sm)', color: 'var(--wf-color-text-secondary)' }}>
                  {u.email} · {u.role ?? 'user'} · {u.banned ? 'inactive' : 'active'}
                </div>
              </div>
              <div style={{ display: 'flex', gap: 'var(--wf-space-xs)' }}>
                <wf-button size="sm" onClick={() => handleRoleChange(u, u.role === 'admin' ? 'user' : 'admin')}>
                  {u.role === 'admin' ? 'Demote' : 'Promote'}
                </wf-button>
                <wf-button size="sm" onClick={() => handleToggleActive(u)}>
                  {u.banned ? 'Reactivate' : 'Deactivate'}
                </wf-button>
                <wf-button size="sm" variant="danger" onClick={() => handleDelete(u)}>
                  Delete
                </wf-button>
              </div>
            </div>
          </wf-list-item>
        ))}
      </wf-list>

      {showCreate && (
        <CreateUserDialog
          onClose={() => setShowCreate(false)}
          onCreated={() => { setShowCreate(false); refresh(); }}
        />
      )}
    </div>
  );
}
```

**Step 3: Create CreateUserDialog**

```tsx
// web/src/components/CreateUserDialog.tsx
import React, { useRef, useEffect, useState } from 'react';
import { createUser } from '../lib/api';

interface Props {
  onClose: () => void;
  onCreated: () => void;
}

export default function CreateUserDialog({ onClose, onCreated }: Props) {
  const dialogRef = useRef<any>(null);
  const [error, setError] = useState('');
  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState('user');

  useEffect(() => {
    dialogRef.current?.show();
    const el = dialogRef.current;
    const handleClose = () => onClose();
    el?.addEventListener('wf-close', handleClose);
    return () => el?.removeEventListener('wf-close', handleClose);
  }, [onClose]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await createUser({
        email,
        password,
        name: displayName || username,
        role,
        data: { username, displayName: displayName || username, type: 'user' },
      });
      onCreated();
    } catch (err: any) {
      setError(err.message);
    }
  };

  return (
    <wf-dialog ref={dialogRef} heading="Create User">
      {error && <wf-banner variant="error" headline={error} />}
      <form onSubmit={handleSubmit}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--wf-space-sm)' }}>
          <label>
            Email
            <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </label>
          <label>
            Username
            <input type="text" required value={username} onChange={(e) => setUsername(e.target.value)} />
          </label>
          <label>
            Display Name
            <input type="text" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
          </label>
          <label>
            Password
            <input type="password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} />
          </label>
          <label>
            Role
            <select value={role} onChange={(e) => setRole(e.target.value)}>
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
          </label>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--wf-space-sm)' }}>
            <wf-button type="button" onClick={onClose}>Cancel</wf-button>
            <button type="submit" style={{ all: 'unset' }}>
              <wf-button variant="primary">Create</wf-button>
            </button>
          </div>
        </div>
      </form>
    </wf-dialog>
  );
}
```

Note: The exact `wf-dialog`, `wf-button`, `wf-banner` APIs may need adaptation — check `@workfort/ui` component docs. React custom element event handling may need `addEventListener` instead of `onClick` for custom events.

**Step 4: Update App.tsx with tab routing**

```tsx
// web/src/App.tsx
import React, { useState } from 'react';
import '@workfort/ui/styles';
import Users from './pages/Users';

type Tab = 'users' | 'service-keys' | 'agent-keys';

export default function App({ connected }: { connected: boolean }) {
  const [tab, setTab] = useState<Tab>('users');

  return (
    <div style={{ padding: 'var(--wf-space-lg)', maxWidth: '960px' }}>
      <div style={{ display: 'flex', gap: 'var(--wf-space-xs)', marginBottom: 'var(--wf-space-lg)' }}>
        <wf-button
          variant={tab === 'users' ? 'primary' : 'default'}
          onClick={() => setTab('users')}
        >
          Users
        </wf-button>
        <wf-button
          variant={tab === 'service-keys' ? 'primary' : 'default'}
          onClick={() => setTab('service-keys')}
        >
          Service Keys
        </wf-button>
        <wf-button
          variant={tab === 'agent-keys' ? 'primary' : 'default'}
          onClick={() => setTab('agent-keys')}
        >
          Agent Keys
        </wf-button>
      </div>

      {tab === 'users' && <Users />}
      {tab === 'service-keys' && <div>Service Keys — Task 8</div>}
      {tab === 'agent-keys' && <div>Agent Keys — Task 9</div>}
    </div>
  );
}
```

**Step 5: Build and verify**

```bash
cd ~/Work/WorkFort/passport/lead/web && pnpm build
```

**Step 6: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add web/src/
git commit -m "feat: add Users page with CRUD and create dialog"
```

---

## Task 8: Service Keys Page

Build the Service Keys CRUD page. Same pattern as Users but for API keys with `type: service`.

**Files:**
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/pages/ServiceKeys.tsx`
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/components/CreateKeyDialog.tsx` — shared between service and agent keys
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/components/KeyRevealDialog.tsx` — shows raw key once
- Modify: `/home/kazw/Work/WorkFort/passport/lead/web/src/App.tsx` — wire up tab

**Step 1: Create shared CreateKeyDialog**

```tsx
// web/src/components/CreateKeyDialog.tsx
import React, { useRef, useEffect, useState } from 'react';
import { createApiKey, listUsers, type User } from '../lib/api';

interface Props {
  type: 'service' | 'agent';
  onClose: () => void;
  onCreated: (rawKey: string) => void;
}

export default function CreateKeyDialog({ type, onClose, onCreated }: Props) {
  const dialogRef = useRef<any>(null);
  const [error, setError] = useState('');
  const [name, setName] = useState('');
  const [users, setUsers] = useState<User[]>([]);
  const [userId, setUserId] = useState('');

  useEffect(() => {
    dialogRef.current?.show();
    const el = dialogRef.current;
    const handleClose = () => onClose();
    el?.addEventListener('wf-close', handleClose);
    // Load users to pick key owner
    listUsers().then((u) => {
      const filtered = u.filter((x) => x.type === type || x.type === undefined);
      setUsers(filtered);
      if (filtered.length > 0) setUserId(filtered[0].id);
    });
    return () => el?.removeEventListener('wf-close', handleClose);
  }, [onClose, type]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const prefix = type === 'service' ? 'wf-svc' : 'wf-agt';
      const result = await createApiKey({
        userId,
        prefix,
        name,
        metadata: { type },
      });
      onCreated(result.key);
    } catch (err: any) {
      setError(err.message);
    }
  };

  return (
    <wf-dialog ref={dialogRef} heading={`Create ${type === 'service' ? 'Service' : 'Agent'} Key`}>
      {error && <wf-banner variant="error" headline={error} />}
      <form onSubmit={handleSubmit}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--wf-space-sm)' }}>
          <label>
            Name
            <input type="text" required value={name} onChange={(e) => setName(e.target.value)} placeholder={`e.g. ${type === 'service' ? 'sharkfin-prod' : 'claude-mcp'}`} />
          </label>
          <label>
            Owner
            <select value={userId} onChange={(e) => setUserId(e.target.value)}>
              {users.map((u) => (
                <option key={u.id} value={u.id}>{u.username ?? u.email}</option>
              ))}
            </select>
          </label>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--wf-space-sm)' }}>
            <wf-button type="button" onClick={onClose}>Cancel</wf-button>
            <button type="submit" style={{ all: 'unset' }}>
              <wf-button variant="primary">Create</wf-button>
            </button>
          </div>
        </div>
      </form>
    </wf-dialog>
  );
}
```

**Step 2: Create KeyRevealDialog**

```tsx
// web/src/components/KeyRevealDialog.tsx
import React, { useRef, useEffect, useState } from 'react';

interface Props {
  rawKey: string;
  onClose: () => void;
}

export default function KeyRevealDialog({ rawKey, onClose }: Props) {
  const dialogRef = useRef<any>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    dialogRef.current?.show();
    const el = dialogRef.current;
    const handleClose = () => onClose();
    el?.addEventListener('wf-close', handleClose);
    return () => el?.removeEventListener('wf-close', handleClose);
  }, [onClose]);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(rawKey);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <wf-dialog ref={dialogRef} heading="API Key Created">
      <wf-banner variant="warning" headline="Copy this key now. It will not be shown again." />
      <div style={{
        fontFamily: 'var(--wf-font-mono)',
        fontSize: 'var(--wf-text-sm)',
        background: 'var(--wf-color-bg-secondary)',
        padding: 'var(--wf-space-md)',
        borderRadius: 'var(--wf-radius-md)',
        wordBreak: 'break-all',
        margin: 'var(--wf-space-md) 0',
      }}>
        {rawKey}
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--wf-space-sm)' }}>
        <wf-button variant="primary" onClick={handleCopy}>
          {copied ? 'Copied!' : 'Copy'}
        </wf-button>
        <wf-button onClick={onClose}>Done</wf-button>
      </div>
    </wf-dialog>
  );
}
```

**Step 3: Create ServiceKeys page**

```tsx
// web/src/pages/ServiceKeys.tsx
import React, { useEffect, useState } from 'react';
import { listAllApiKeys, deleteApiKey, type ApiKey } from '../lib/api';
import CreateKeyDialog from '../components/CreateKeyDialog';
import KeyRevealDialog from '../components/KeyRevealDialog';

export default function ServiceKeys() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [revealKey, setRevealKey] = useState('');

  const refresh = async () => {
    try {
      const all = await listAllApiKeys();
      setKeys(all.filter((k) => k.metadata?.type === 'service'));
      setError('');
    } catch (e: any) {
      setError(e.message);
    }
  };

  useEffect(() => { refresh(); }, []);

  const handleRevoke = async (key: ApiKey) => {
    if (!confirm(`Revoke service key "${key.name}"?`)) return;
    try {
      await deleteApiKey(key.id);
      await refresh();
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--wf-space-md)' }}>
        <h2 style={{ margin: 0 }}>Service Keys</h2>
        <wf-button variant="primary" onClick={() => setShowCreate(true)}>Create Service Key</wf-button>
      </div>

      {error && <wf-banner variant="error" headline={error} />}

      <wf-list>
        {keys.map((k) => (
          <wf-list-item key={k.id}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
              <div>
                <strong>{k.name}</strong>
                <div style={{ fontSize: 'var(--wf-text-sm)', color: 'var(--wf-color-text-secondary)' }}>
                  {k.prefix}••• · Created {new Date(k.createdAt).toLocaleDateString()}
                </div>
              </div>
              <wf-button size="sm" variant="danger" onClick={() => handleRevoke(k)}>
                Revoke
              </wf-button>
            </div>
          </wf-list-item>
        ))}
        {keys.length === 0 && (
          <wf-list-item>
            <span style={{ color: 'var(--wf-color-text-secondary)' }}>No service keys. Create one to connect a service like Sharkfin.</span>
          </wf-list-item>
        )}
      </wf-list>

      {showCreate && (
        <CreateKeyDialog
          type="service"
          onClose={() => setShowCreate(false)}
          onCreated={(key) => { setShowCreate(false); setRevealKey(key); refresh(); }}
        />
      )}

      {revealKey && (
        <KeyRevealDialog rawKey={revealKey} onClose={() => setRevealKey('')} />
      )}
    </div>
  );
}
```

**Step 4: Wire up in App.tsx**

Replace the `{tab === 'service-keys' && <div>...}` placeholder with:

```tsx
import ServiceKeys from './pages/ServiceKeys';
// ...
{tab === 'service-keys' && <ServiceKeys />}
```

**Step 5: Build and verify**

```bash
cd ~/Work/WorkFort/passport/lead/web && pnpm build
```

**Step 6: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add web/src/
git commit -m "feat: add Service Keys page with create and revoke"
```

---

## Task 9: Agent Keys Page

Nearly identical to Service Keys but for `type: agent`.

**Files:**
- Create: `/home/kazw/Work/WorkFort/passport/lead/web/src/pages/AgentKeys.tsx`
- Modify: `/home/kazw/Work/WorkFort/passport/lead/web/src/App.tsx` — wire up tab

**Step 1: Create AgentKeys page**

```tsx
// web/src/pages/AgentKeys.tsx
import React, { useEffect, useState } from 'react';
import { listAllApiKeys, deleteApiKey, type ApiKey } from '../lib/api';
import CreateKeyDialog from '../components/CreateKeyDialog';
import KeyRevealDialog from '../components/KeyRevealDialog';

export default function AgentKeys() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [revealKey, setRevealKey] = useState('');

  const refresh = async () => {
    try {
      const all = await listAllApiKeys();
      setKeys(all.filter((k) => k.metadata?.type === 'agent'));
      setError('');
    } catch (e: any) {
      setError(e.message);
    }
  };

  useEffect(() => { refresh(); }, []);

  const handleRevoke = async (key: ApiKey) => {
    if (!confirm(`Revoke agent key "${key.name}"?`)) return;
    try {
      await deleteApiKey(key.id);
      await refresh();
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--wf-space-md)' }}>
        <h2 style={{ margin: 0 }}>Agent Keys</h2>
        <wf-button variant="primary" onClick={() => setShowCreate(true)}>Create Agent Key</wf-button>
      </div>

      {error && <wf-banner variant="error" headline={error} />}

      <wf-list>
        {keys.map((k) => (
          <wf-list-item key={k.id}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
              <div>
                <strong>{k.name}</strong>
                <div style={{ fontSize: 'var(--wf-text-sm)', color: 'var(--wf-color-text-secondary)' }}>
                  {k.prefix}••• · Created {new Date(k.createdAt).toLocaleDateString()}
                </div>
              </div>
              <wf-button size="sm" variant="danger" onClick={() => handleRevoke(k)}>
                Revoke
              </wf-button>
            </div>
          </wf-list-item>
        ))}
        {keys.length === 0 && (
          <wf-list-item>
            <span style={{ color: 'var(--wf-color-text-secondary)' }}>No agent keys. Create one for MCP bridges or AI agents.</span>
          </wf-list-item>
        )}
      </wf-list>

      {showCreate && (
        <CreateKeyDialog
          type="agent"
          onClose={() => setShowCreate(false)}
          onCreated={(key) => { setShowCreate(false); setRevealKey(key); refresh(); }}
        />
      )}

      {revealKey && (
        <KeyRevealDialog rawKey={revealKey} onClose={() => setRevealKey('')} />
      )}
    </div>
  );
}
```

**Step 2: Wire up in App.tsx**

```tsx
import AgentKeys from './pages/AgentKeys';
// ...
{tab === 'agent-keys' && <AgentKeys />}
```

**Step 3: Build and verify**

```bash
cd ~/Work/WorkFort/passport/lead/web && pnpm build
```

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/passport/lead
git add web/src/
git commit -m "feat: add Agent Keys page with create and revoke"
```

---

## Task 10: Sharkfin Identity Type Enforcement

Update Sharkfin's auth middleware to check the identity `type` from Passport and map it to the appropriate Sharkfin role.

**Files:**
- Modify: `/home/kazw/Work/WorkFort/sharkfin/lead/pkg/daemon/mcp_server.go` — use type for role mapping
- Modify: `/home/kazw/Work/WorkFort/sharkfin/lead/pkg/daemon/ws_handler.go` — same
- Create: `/home/kazw/Work/WorkFort/sharkfin/lead/pkg/daemon/mcp_server_test.go` (or modify existing) — test type mapping

**Step 1: Write test for identity type → role mapping**

Add to existing test file or create new:

```go
func TestIdentityTypeRoleMapping(t *testing.T) {
	tests := []struct {
		identityType string
		expectedRole string
	}{
		{"user", "user"},
		{"service", "service"},
		{"agent", "agent"},
		{"", "user"},          // default
		{"unknown", "user"},   // fallback
	}
	for _, tt := range tests {
		t.Run(tt.identityType, func(t *testing.T) {
			role := mapIdentityTypeToRole(tt.identityType)
			if role != tt.expectedRole {
				t.Errorf("mapIdentityTypeToRole(%q) = %q, want %q", tt.identityType, role, tt.expectedRole)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd ~/Work/WorkFort/sharkfin/lead && go test ./pkg/daemon/ -run TestIdentityTypeRoleMapping -v
```

Expected: FAIL (function doesn't exist)

**Step 3: Implement mapIdentityTypeToRole**

In `mcp_server.go` (or a new `identity.go` file in the daemon package):

```go
// mapIdentityTypeToRole maps a Passport identity type to a Sharkfin role.
func mapIdentityTypeToRole(identityType string) string {
	switch identityType {
	case "service":
		return "service"
	case "agent":
		return "agent"
	case "user":
		return "user"
	default:
		return "user"
	}
}
```

**Step 4: Update MCP server auto-provision**

In `mcp_server.go`, replace the role assignment at line ~116:

```go
// Before:
role := identity.Type
if role == "" {
    role = "user"
}

// After:
role := mapIdentityTypeToRole(identity.Type)
```

**Step 5: Run test to verify it passes**

```bash
cd ~/Work/WorkFort/sharkfin/lead && go test ./pkg/daemon/ -run TestIdentityTypeRoleMapping -v
```

Expected: PASS

**Step 6: Run full test suite**

```bash
cd ~/Work/WorkFort/sharkfin/lead && mise run test
```

**Step 7: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git add pkg/daemon/
git commit -m "feat: map identity type to Sharkfin role on auto-provision"
```

---

## Task 11: End-to-End Integration Test

Verify the full bootstrap flow works: Passport health → shell discovers admin UI → create service key → configure Sharkfin.

**This task is manual/interactive.** Use Playwright MCP to navigate and verify.

**Step 1: Build Passport web UI**

```bash
cd ~/Work/WorkFort/passport/lead/web && pnpm build
```

**Step 2: Restart Passport with UI serving**

Ensure Passport is serving `web/dist/` at `/ui/*`. This may require restarting the Passport VM or updating the Nexus VM config.

**Step 3: Rebuild and reinstall Sharkfin**

```bash
cd ~/Work/WorkFort/sharkfin/lead && mise run build && mise run install:local
```

**Step 4: Rebuild BFF**

```bash
cd ~/Work/WorkFort/scope/lead && mise run build && mise run install:local
```

**Step 5: Verify via Playwright**

1. Navigate to `http://127.0.0.1:16100` — should show shell
2. Sign in as admin
3. Verify "Admin" appears in navigation (admin user)
4. Click "Admin" → should load Passport React MF remote
5. Navigate to Service Keys tab → create a key
6. Navigate to Agent Keys tab → create a key
7. Verify Users tab shows the admin user

**Step 6: Commit any fixes**

If integration reveals issues, fix and commit each one separately with descriptive messages.

---

## Summary

| Task | Repo | Description |
|------|------|-------------|
| 1 | Scope (shell) | Framework-agnostic service mount (mount/unmount) |
| 2 | Scope (BFF + shell) | Admin-only service filtering |
| 3 | Passport | /ui/health update + static asset serving |
| 4 | Passport | Last-admin guard |
| 5 | Passport | Custom admin API key listing route |
| 6 | Passport | React MF remote scaffold |
| 7 | Passport | Users page |
| 8 | Passport | Service Keys page |
| 9 | Passport | Agent Keys page |
| 10 | Sharkfin | Identity type → role mapping |
| 11 | All | End-to-end integration test |
