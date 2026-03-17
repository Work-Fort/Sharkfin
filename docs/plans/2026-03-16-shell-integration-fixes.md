# Shell Integration Fixes — Cross-Repo Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the two blockers preventing Sharkfin's web UI from loading in the WorkFort shell: Passport missing `/ui/health` endpoint (service discovery), and BFF auth-wrapping static UI assets.

**Architecture:** Passport adds a `/ui/health` route returning a 503 JSON manifest with `name: "auth"` — no UI but discoverable. The BFF exempts `/ui/` paths from token conversion so `remoteEntry.js` and other static assets load without auth. Both fixes are independently deployable.

**Tech Stack:** Passport: Hono (TypeScript), Scope BFF: Go `net/http`, Sharkfin: no changes needed.

**Repos touched:**
- `passport/lead` — Passport identity provider
- `scope/lead` — WorkFort shell + BFF

---

## Coverage Checklist

| Issue # | Description | Task |
|---------|-------------|------|
| 1 | Passport missing `/ui/health` | Task 1 |
| 2 | BFF auth-wraps static UI assets | Task 2 |

---

### Task 1: Passport — Add `/ui/health` Service Discovery Endpoint

**Repo:** `passport/lead`

**Files:**
- Modify: `src/index.ts`
- Create: `src/test/health.test.ts`

The WorkFort BFF's `ServiceTracker` probes every configured service URL at `GET /ui/health`. It expects a JSON response:

```json
{
  "status": "ok",
  "name": "auth",
  "label": "Auth",
  "route": ""
}
```

The status code determines `ui` in the service list:
- **200** → `ui: true` (service has a frontend)
- **503** → `ui: false` (service exists but has no frontend)

Passport has no UI, so it returns 503 with the manifest. The `name: "auth"` is critical — the BFF's `fort_router.go` calls `tracker.ServiceByName("auth")` to find the auth provider for token conversion.

**Step 1: Write failing test**

Create `src/test/health.test.ts`:

```typescript
import { describe, it, expect } from "vitest";

describe("GET /ui/health", () => {
  it("returns 503 with auth service manifest", async () => {
    // Import the app — Hono app from index.ts
    const { app } = await import("../index.js");
    const res = await app.request("/ui/health");

    expect(res.status).toBe(503);

    const body = await res.json();
    expect(body.status).toBe("ok");
    expect(body.name).toBe("auth");
    expect(body.label).toBe("Auth");
    expect(body.route).toBe("");
  });
});
```

Note: This requires the Hono app to be exported from `src/index.ts`. Currently only the server is started. The app needs to be exported for testability.

**Step 2: Run test to verify it fails**

Run: `cd passport/lead && npx vitest run src/test/health.test.ts`

If vitest is not installed, add it: `pnpm add -D vitest`

Expected: FAIL — either `/ui/health` returns 404 or `app` is not exported.

**Step 3: Implement**

Modify `src/index.ts`:

1. Export the `app` instance (add `export` before `const app`):
```typescript
export const app = new Hono();
```

2. Add the `/ui/health` route after the existing `/health` route:
```typescript
// Service discovery — the WorkFort BFF probes this to discover services.
// 503 = no UI, but the manifest lets the BFF register this as the auth provider.
app.get("/ui/health", (c) => {
  return c.json(
    { status: "ok", name: "auth", label: "Auth", route: "" },
    503,
  );
});
```

The full route section becomes:
```typescript
export const app = new Hono();

// Health check — public, outside auth
app.get("/health", (c) => c.json({ status: "ok" }));

// Service discovery — WorkFort BFF probes this to find the auth provider.
app.get("/ui/health", (c) => {
  return c.json(
    { status: "ok", name: "auth", label: "Auth", route: "" },
    503,
  );
});

// Adapter routes take priority (registered before the catch-all)
app.route("/", verifyApiKeyRoute);

// Better Auth handles everything else under /v1/*
app.on(["GET", "POST"], "/v1/*", (c) => {
  return auth.handler(c.req.raw);
});
```

**Step 4: Run test to verify it passes**

Run: `cd passport/lead && npx vitest run src/test/health.test.ts`
Expected: PASS.

**Step 5: Rebuild and deploy to Nexus VM**

```bash
cd passport/lead && pnpm build
```

Then rebuild the container image or copy the built `dist/` to the VM. If using the Nexus VM directly, the updated code needs to be deployed.

**Step 6: Verify live**

```bash
curl -s -w "\n%{http_code}" http://passport.nexus:3000/ui/health
```

Expected:
```json
{"status":"ok","name":"auth","label":"Auth","route":""}
503
```

**Step 7: Commit**

```bash
cd passport/lead
git add src/index.ts src/test/health.test.ts
git commit -m "feat: add /ui/health endpoint for WorkFort service discovery"
```

---

### Task 2: BFF — Exempt Static UI Assets from Auth

**Repo:** `scope/lead`

**Files:**
- Modify: `internal/infra/httpapi/handler.go`
- Modify: `internal/infra/httpapi/handler_test.go`

The BFF's `bffMiddleware` wraps ALL proxied requests with token conversion. When `TokenConverter` is nil (no auth service configured), it returns 401 for everything — including static UI assets like `remoteEntry.js`, CSS, and JS bundles. These files are public and don't contain user-specific data.

**Step 1: Write failing test**

Add to `internal/infra/httpapi/handler_test.go`:

```go
func TestHandler_UIAssetsServedWithoutAuth(t *testing.T) {
	tracker, cleanup := newTestTracker(t)
	defer cleanup()
	fort := newTestFort(tracker)

	// Pass nil token converter — simulates no auth service configured.
	handler := httpapi.NewHandler(fort, tracker, nil, nil)

	// Request a UI asset path for sharkfin.
	req := httptest.NewRequest(http.MethodGet, "/api/sharkfin/ui/remoteEntry.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should NOT be 401 — static assets bypass auth.
	// The proxy will forward to the mock server which returns 200.
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("UI asset request should not require auth, got 401")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd scope/lead && mise run test`
Expected: FAIL — the handler returns 401 because `tc` is nil.

**Step 3: Implement**

In `internal/infra/httpapi/handler.go`, modify `bffMiddleware` to pass through requests for `/ui/` paths without token conversion:

```go
func bffMiddleware(fortName string, tc *TokenConverter, proxy http.Handler, wsHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for WebSocket upgrade.
		if wsHandler != nil && isWebSocketUpgrade(r) {
			if tc != nil {
				token, err := tc.Token(r)
				if err != nil {
					writeAuthError(w, err, fortName)
					return
				}
				r.Header.Set("Authorization", "Bearer "+token)
			}
			wsHandler.ServeHTTP(w, r)
			return
		}

		// Static UI assets (remoteEntry.js, CSS, JS) are public — no auth needed.
		if isUIAssetRequest(r) {
			proxy.ServeHTTP(w, r)
			return
		}

		// Regular HTTP — BFF conversion.
		if tc == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "auth: no token converter configured"})
			return
		}

		token, err := tc.Token(r)
		if err != nil {
			writeAuthError(w, err, fortName)
			return
		}

		r.Header.Set("Authorization", "Bearer "+token)
		proxy.ServeHTTP(w, r)
	})
}
```

Add the helper function:

```go
// isUIAssetRequest returns true for requests targeting a service's /ui/ path.
// These are static assets (remoteEntry.js, CSS, JS bundles) that don't need auth.
// The request path has already had the fort prefix stripped by the mux, so it
// looks like /api/{service}/ui/...
func isUIAssetRequest(r *http.Request) bool {
	// Match /api/{name}/ui/ prefix. Split: ["", "api", "{name}", "ui", ...]
	parts := strings.SplitN(r.URL.Path, "/", 5)
	return len(parts) >= 4 && parts[1] == "api" && parts[3] == "ui"
}
```

Add `"strings"` to the imports.

**Step 4: Run test to verify it passes**

Run: `cd scope/lead && mise run test`
Expected: All tests pass, including the new one.

**Step 5: Commit**

```bash
cd scope/lead
git add internal/infra/httpapi/handler.go internal/infra/httpapi/handler_test.go
git commit -m "fix: exempt /ui/ static assets from BFF auth requirement"
```

---

### Task 3: Integration Verification

After Tasks 1 and 2 are deployed, re-run the integration test procedure from the scope team lead's instructions:

**Step 1: Verify Passport discovery**

```bash
curl -s http://passport.nexus:3000/ui/health
```

Expected: 503 with `{"status":"ok","name":"auth",...}`

**Step 2: Verify BFF discovers both services**

```bash
curl -s http://127.0.0.1:16100/forts/local/api/services | python3 -m json.tool
```

Expected: Both `sharkfin` (ui: true) and `auth` (ui: false) in the services list.

**Step 3: Verify remoteEntry.js loads through BFF**

```bash
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:16100/forts/local/api/sharkfin/ui/remoteEntry.js
```

Expected: 200 (not 401).

**Step 4: Verify in browser with Playwright**

1. Navigate to `http://127.0.0.1:16100`
2. Take a snapshot — the shell should load with "Chat" in the nav
3. Navigate to `/chat` — the Sharkfin MF remote should render (loading state or chat UI, not "unavailable")
4. Check `browser_console_messages` for MF errors — should be none

**Step 5: Document results**

Update `docs/2026-03-16-shell-integration-issues.md` with the verification results. Mark Issues 1 and 2 as resolved. Note any new issues discovered.
