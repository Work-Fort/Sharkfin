# Onboarding Flow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the first-run onboarding and sign-in flows so users can authenticate in the WorkFort shell and access services like Sharkfin.

**Architecture:** Passport adds a `setup_mode` flag to `/ui/health` and a server-side guard on sign-up. The BFF passes `setup_mode` through to the shell. The shell adds setup and sign-in views that call Passport's API through the BFF auth proxy. Sharkfin handles the `connected` prop gracefully.

**Tech Stack:** Passport: Hono + Better Auth (TypeScript). Scope BFF: Go `net/http`. Shell: SolidJS + `@workfort/ui`. Sharkfin: SolidJS.

**Repos:**
- `passport/lead` — Tasks 1-2
- `scope/lead` — Tasks 3-5
- `sharkfin/lead` — Task 6

---

## Coverage Checklist

| Requirement | Task |
|---|---|
| `setup_mode` in health endpoint | Task 1 |
| Server-side sign-up guard | Task 2 |
| BFF passes `setup_mode` to shell | Task 3 |
| Shell setup view (first run) | Task 4 |
| Shell sign-in view (returning users) | Task 5 |
| Sharkfin handles auth failure gracefully | Task 6 |

---

### Task 1: Passport — `setup_mode` in `/ui/health`

**Repo:** `passport/lead`

**Files:**
- Modify: `src/app.ts`
- Modify: `src/test/health.test.ts`

**Step 1: Write failing test**

Add to `src/test/health.test.ts`:

```typescript
describe("GET /ui/health setup_mode", () => {
  it("returns setup_mode: true when no users exist", async () => {
    const res = await app.request("/ui/health");
    const body = await res.json();
    expect(body.setup_mode).toBe(true);
  });
});
```

Note: The test environment starts with an empty database (no users). If the existing test suite seeds users before this test runs, this test may need to run first or use a separate database.

**Step 2: Run test to verify it fails**

Run: `cd passport/lead && npx vitest run src/test/health.test.ts`
Expected: FAIL — `setup_mode` is not in the response.

**Step 3: Implement**

In `src/app.ts`, modify the `/ui/health` handler to check the user count:

```typescript
import { auth } from "./auth.js";

app.get("/ui/health", async (c) => {
  // Check if any users exist — determines setup mode.
  const userCount = await auth.api.listUsers({
    query: { limit: 1 },
    headers: c.req.raw.headers,
  }).then((res: any) => res.users?.length ?? 0)
    .catch(() => 0);

  const body: Record<string, any> = {
    status: "ok",
    name: "auth",
    label: "Auth",
    route: "",
  };

  if (userCount === 0) {
    body.setup_mode = true;
  }

  return c.json(body, 503);
});
```

Note: Better Auth's `listUsers` is an admin API. In setup mode (no users), there's no admin to authorize the request. Check if `auth.api.listUsers` works without auth when no users exist. If not, use a direct database query instead:

```typescript
// Alternative: direct DB count
const db = (auth as any).options.database;
// This depends on the database adapter — may need adjustment
```

If the Better Auth API doesn't work, use Hono middleware to query the database directly. The implementer should check what works and use the simplest approach.

**Step 4: Run test to verify it passes**

Run: `cd passport/lead && npx vitest run src/test/health.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add src/app.ts src/test/health.test.ts
git commit -m "feat: add setup_mode flag to /ui/health when no users exist"
```

---

### Task 2: Passport — Server-Side Sign-Up Guard

**Repo:** `passport/lead`

**Files:**
- Modify: `src/app.ts`
- Create: `src/test/signup-guard.test.ts`

Better Auth's `POST /v1/sign-up/email` is open by default — anyone can create an account. We need to restrict it: only allow unauthenticated sign-up when no users exist (setup mode). Once the first user is created, sign-up requires admin auth.

**Step 1: Write failing test**

Create `src/test/signup-guard.test.ts`:

```typescript
import { describe, it, expect } from "vitest";
import { app } from "../app.js";

describe("sign-up guard", () => {
  it("allows sign-up when no users exist", async () => {
    const res = await app.request("/v1/sign-up/email", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: "first@example.com",
        password: "testpassword123",
        name: "First User",
        username: "firstuser",
        displayName: "First User",
      }),
    });
    expect(res.status).toBeLessThan(400);
  });

  it("rejects unauthenticated sign-up after first user exists", async () => {
    // First user was created above — now try without auth
    const res = await app.request("/v1/sign-up/email", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: "second@example.com",
        password: "testpassword123",
        name: "Second User",
        username: "seconduser",
        displayName: "Second User",
      }),
    });
    // Should be rejected — 403 or 401
    expect(res.status).toBeGreaterThanOrEqual(400);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd passport/lead && npx vitest run src/test/signup-guard.test.ts`
Expected: The second test FAILS — sign-up succeeds without auth even after first user exists.

**Step 3: Implement**

Add a Hono middleware in `src/app.ts` before the Better Auth catch-all that intercepts `POST /v1/sign-up/email`:

```typescript
// Guard: sign-up is only open when no users exist (setup mode).
// After the first user, sign-up requires admin auth (handled by Better Auth's admin plugin).
app.post("/v1/sign-up/email", async (c, next) => {
  const userCount = await /* same user count check as /ui/health */;
  if (userCount > 0) {
    // Users exist — check if request has admin auth.
    // If no auth, reject. Better Auth's admin plugin handles authorized creation.
    const session = await auth.api.getSession({ headers: c.req.raw.headers }).catch(() => null);
    if (!session) {
      return c.json({ error: "Sign-up requires admin authorization" }, 403);
    }
  }
  // Setup mode (no users) or authenticated admin — allow through to Better Auth.
  return next();
});
```

Note: The route must be registered BEFORE the Better Auth catch-all (`app.on(["GET", "POST"], "/v1/*", ...)`). The `next()` call lets it fall through to Better Auth for the actual sign-up logic.

Also: the first user created should get the admin role. Check if Better Auth's admin plugin auto-assigns admin to the first user, or if we need to call `auth.api.setRole` after creation. The implementer should verify this.

**Step 4: Run test to verify it passes**

Run: `cd passport/lead && npx vitest run src/test/signup-guard.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add src/app.ts src/test/signup-guard.test.ts
git commit -m "feat: guard sign-up endpoint — open only in setup mode"
```

---

### Task 3: BFF — Pass `setup_mode` to Shell

**Repo:** `scope/lead`

**Files:**
- Modify: `internal/infra/httpapi/tracker.go`
- Modify: `internal/infra/httpapi/handler_test.go`

**Step 1: Write failing test**

Add to `handler_test.go`:

```go
func TestHandler_ServicesIncludesSetupMode(t *testing.T) {
	// Create a mock auth service that returns setup_mode: true
	authWithSetup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"name":       "auth",
			"label":      "Auth",
			"route":      "",
			"setup_mode": true,
		})
	}))
	defer authWithSetup.Close()

	tracker := httpapi.NewServiceTracker([]string{authWithSetup.URL})
	tracker.InitialProbe(context.Background())

	fort := domain.Fort{Name: "local", Local: true}
	handler := httpapi.NewHandler(fort, tracker, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp struct {
		Services []struct {
			Name      string `json:"name"`
			SetupMode bool   `json:"setup_mode,omitempty"`
		} `json:"services"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Services) == 0 {
		t.Fatal("expected at least one service")
	}
	if !resp.Services[0].SetupMode {
		t.Fatal("expected setup_mode to be true for auth service")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd scope/lead && mise run test`
Expected: FAIL — `setup_mode` not in the `TrackedService` struct or JSON output.

**Step 3: Implement**

In `tracker.go`, add `SetupMode` to `TrackedService`:

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
	WSPaths   []string `json:"-"`

	wsRefCount int32
	hasWS      bool
}
```

In the `probeOne` function, parse `setup_mode` from the health response and store it:

```go
var health struct {
	Status    string `json:"status"`
	SetupMode bool   `json:"setup_mode,omitempty"`
	frontend.Manifest
}
```

Then when updating the service entry, set `SetupMode`:

```go
// In the "update existing service" block:
t.services[idx].SetupMode = health.SetupMode

// In the "new service" block:
SetupMode: health.SetupMode,
```

**Step 4: Run test to verify it passes**

Run: `cd scope/lead && mise run test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add internal/infra/httpapi/tracker.go internal/infra/httpapi/handler_test.go
git commit -m "feat: pass setup_mode from service health to shell"
```

---

### Task 4: Shell — Setup View

**Repo:** `scope/lead`

**Files:**
- Create: `web/shell/src/components/setup-form.tsx`
- Modify: `web/shell/src/app.tsx`
- Modify: `web/shell/src/lib/api.ts`
- Modify: `web/shell/src/stores/services.ts`

**Step 1: Add `setup_mode` to the ServiceInfo type**

In `web/shell/src/lib/api.ts`, add to `ServiceInfo`:

```typescript
export interface ServiceInfo {
  name: string;
  label: string;
  route: string;
  enabled: boolean;
  ui: boolean;
  connected: boolean;
  setup_mode?: boolean;
}
```

**Step 2: Add setup detection to services store**

In `web/shell/src/stores/services.ts`, add a derived signal:

```typescript
const [setupMode, setSetupMode] = createSignal(false);

// In handlePollResult:
const authSvc = res.services.find((s) => s.name === 'auth');
setSetupMode(authSvc?.setup_mode === true);

// Export:
export const isSetupMode = setupMode;
```

**Step 3: Create setup form component**

Create `web/shell/src/components/setup-form.tsx`:

```tsx
import { createSignal, Show, type Component } from 'solid-js';

interface SetupFormProps {
  fort: string;
  onComplete: () => void;
}

const SetupForm: Component<SetupFormProps> = (props) => {
  const [email, setEmail] = createSignal('');
  const [username, setUsername] = createSignal('');
  const [password, setPassword] = createSignal('');
  const [confirm, setConfirm] = createSignal('');
  const [error, setError] = createSignal('');
  const [loading, setLoading] = createSignal(false);

  async function handleSubmit(e: Event) {
    e.preventDefault();
    setError('');

    if (password() !== confirm()) {
      setError('Passwords do not match');
      return;
    }
    if (!email() || !username() || !password()) {
      setError('All fields are required');
      return;
    }

    setLoading(true);
    try {
      const res = await fetch(`/forts/${props.fort}/api/auth/v1/sign-up/email`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          email: email(),
          password: password(),
          name: username(),
          username: username(),
          displayName: username(),
        }),
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        setError(body.message ?? body.error ?? `Sign-up failed (${res.status})`);
        return;
      }

      // Sign-up succeeded — now sign in to get the session cookie.
      const signIn = await fetch(`/forts/${props.fort}/api/auth/v1/sign-in/email`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          email: email(),
          password: password(),
        }),
      });

      if (!signIn.ok) {
        setError('Account created but sign-in failed. Please refresh and sign in.');
        return;
      }

      // Session cookie is set by the BFF auth proxy.
      props.onComplete();
    } catch (err: any) {
      setError(err.message ?? 'Network error');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style="max-width: 24rem; margin: 4rem auto; padding: var(--wf-space-lg);">
      <h1 style="font-size: var(--wf-text-lg); font-weight: var(--wf-weight-semibold); margin-bottom: var(--wf-space-md); color: var(--wf-color-text);">
        Create Admin Account
      </h1>
      <p style="font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary); margin-bottom: var(--wf-space-lg);">
        Set up the first account to get started with WorkFort.
      </p>

      <Show when={error()}>
        <wf-banner variant="error" headline={error()} style="margin-bottom: var(--wf-space-md);" />
      </Show>

      <form on:submit={handleSubmit} style="display: flex; flex-direction: column; gap: var(--wf-space-md);">
        <label style="display: flex; flex-direction: column; gap: var(--wf-space-xs); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          Email
          <input
            type="email"
            required
            value={email()}
            on:input={(e: Event) => setEmail((e.target as HTMLInputElement).value)}
            style="padding: var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm);"
          />
        </label>

        <label style="display: flex; flex-direction: column; gap: var(--wf-space-xs); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          Username
          <input
            type="text"
            required
            value={username()}
            on:input={(e: Event) => setUsername((e.target as HTMLInputElement).value)}
            style="padding: var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm);"
          />
        </label>

        <label style="display: flex; flex-direction: column; gap: var(--wf-space-xs); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          Password
          <input
            type="password"
            required
            value={password()}
            on:input={(e: Event) => setPassword((e.target as HTMLInputElement).value)}
            style="padding: var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm);"
          />
        </label>

        <label style="display: flex; flex-direction: column; gap: var(--wf-space-xs); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          Confirm Password
          <input
            type="password"
            required
            value={confirm()}
            on:input={(e: Event) => setConfirm((e.target as HTMLInputElement).value)}
            style="padding: var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm);"
          />
        </label>

        <wf-button type="submit" disabled={loading()} style="margin-top: var(--wf-space-sm);">
          {loading() ? 'Creating...' : 'Create Account'}
        </wf-button>
      </form>
    </div>
  );
};

export default SetupForm;
```

**Step 4: Wire into app.tsx**

In `web/shell/src/app.tsx`, import the setup view and setup mode signal:

```typescript
import { isSetupMode } from './stores/services';
import SetupForm from './components/setup-form';
```

In `FortShell`, before rendering children, check for setup mode:

```tsx
const FortShell: Component = (props: { children?: any }) => {
  const params = useParams<{ fort: string }>();
  const [sidebarComponent, setSidebarComponent] = createSignal<(() => any) | undefined>();

  createEffect(() => {
    const fort = params.fort;
    startPolling(fort);
  });
  onCleanup(() => stopPolling());

  return (
    <FortShellContext.Provider value={{ setSidebarComponent }}>
      <Show when={!isSetupMode()} fallback={
        <SetupForm fort={params.fort} onComplete={() => {
          // Re-fetch services — setup_mode should be gone now.
          startPolling(params.fort);
        }} />
      }>
        <ShellLayout sidebar={sidebarComponent()}>{props.children}</ShellLayout>
      </Show>
    </FortShellContext.Provider>
  );
};
```

**Step 5: Commit**

```bash
git add web/shell/src/components/setup-form.tsx web/shell/src/app.tsx web/shell/src/lib/api.ts web/shell/src/stores/services.ts
git commit -m "feat: add setup view for first-run admin account creation"
```

---

### Task 5: Shell — Sign-In View

**Repo:** `scope/lead`

**Files:**
- Create: `web/shell/src/components/sign-in-form.tsx`
- Modify: `web/shell/src/app.tsx`
- Modify: `web/shell/src/stores/services.ts`

**Step 1: Add auth state detection**

The shell needs to know when services return 401 (user not signed in). In `web/shell/src/stores/services.ts`, track auth errors:

```typescript
const [needsAuth, setNeedsAuth] = createSignal(false);

// In the fetch call, catch 401:
export function startPolling(fort: string): void {
  if (activeFort !== fort) {
    stopPolling();
    prevConnected = new Map();
    setServiceList([]);
    setConflictList([]);
    setNeedsAuth(false);
  }
  activeFort = fort;

  const poll = () => {
    fetchServices(fort)
      .then((res) => {
        setNeedsAuth(false);
        handlePollResult(res);
      })
      .catch((err) => {
        // fetchServices throws on non-OK responses.
        // If we get here, services endpoint itself is unreachable.
        console.error(err);
      });
  };

  poll();
  intervalId = setInterval(poll, POLL_INTERVAL);
}

export const isAuthRequired = needsAuth;
```

Note: The `/api/services` endpoint itself doesn't require auth (it's a shell endpoint, not a service endpoint). The 401 happens when individual service API calls fail. The sign-in form should show when the user tries to interact with a service and gets 401.

Actually, simpler approach: detect the lack of session by attempting a BFF-proxied request. Or even simpler — the shell should show the sign-in form when there's no session cookie present. Check for the cookie directly:

```typescript
function hasSessionCookie(): boolean {
  return document.cookie.includes('better-auth.session_token');
}

const [needsAuth, setNeedsAuth] = createSignal(!hasSessionCookie());
export const isAuthRequired = needsAuth;
```

**Step 2: Create sign-in form component**

Create `web/shell/src/components/sign-in-form.tsx`:

```tsx
import { createSignal, Show, type Component } from 'solid-js';

interface SignInFormProps {
  fort: string;
  onComplete: () => void;
}

const SignInForm: Component<SignInFormProps> = (props) => {
  const [email, setEmail] = createSignal('');
  const [password, setPassword] = createSignal('');
  const [error, setError] = createSignal('');
  const [loading, setLoading] = createSignal(false);

  async function handleSubmit(e: Event) {
    e.preventDefault();
    setError('');

    if (!email() || !password()) {
      setError('Email and password are required');
      return;
    }

    setLoading(true);
    try {
      const res = await fetch(`/forts/${props.fort}/api/auth/v1/sign-in/email`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          email: email(),
          password: password(),
        }),
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        setError(body.message ?? body.error ?? `Sign-in failed (${res.status})`);
        return;
      }

      // Session cookie set by BFF auth proxy.
      props.onComplete();
    } catch (err: any) {
      setError(err.message ?? 'Network error');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style="max-width: 24rem; margin: 4rem auto; padding: var(--wf-space-lg);">
      <h1 style="font-size: var(--wf-text-lg); font-weight: var(--wf-weight-semibold); margin-bottom: var(--wf-space-md); color: var(--wf-color-text);">
        Sign In
      </h1>

      <Show when={error()}>
        <wf-banner variant="error" headline={error()} style="margin-bottom: var(--wf-space-md);" />
      </Show>

      <form on:submit={handleSubmit} style="display: flex; flex-direction: column; gap: var(--wf-space-md);">
        <label style="display: flex; flex-direction: column; gap: var(--wf-space-xs); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          Email
          <input
            type="email"
            required
            value={email()}
            on:input={(e: Event) => setEmail((e.target as HTMLInputElement).value)}
            style="padding: var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm);"
          />
        </label>

        <label style="display: flex; flex-direction: column; gap: var(--wf-space-xs); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          Password
          <input
            type="password"
            required
            value={password()}
            on:input={(e: Event) => setPassword((e.target as HTMLInputElement).value)}
            style="padding: var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm);"
          />
        </label>

        <wf-button type="submit" disabled={loading()} style="margin-top: var(--wf-space-sm);">
          {loading() ? 'Signing in...' : 'Sign In'}
        </wf-button>
      </form>
    </div>
  );
};

export default SignInForm;
```

**Step 3: Wire into app.tsx**

Import and add the sign-in gate in `FortShell`:

```typescript
import SignInForm from './components/sign-in-form';
import { isSetupMode, isAuthRequired } from './stores/services';
```

Update `FortShell`:

```tsx
const FortShell: Component = (props: { children?: any }) => {
  const params = useParams<{ fort: string }>();
  const [sidebarComponent, setSidebarComponent] = createSignal<(() => any) | undefined>();

  createEffect(() => {
    startPolling(params.fort);
  });
  onCleanup(() => stopPolling());

  return (
    <FortShellContext.Provider value={{ setSidebarComponent }}>
      <Show when={!isSetupMode()} fallback={
        <SetupForm fort={params.fort} onComplete={() => startPolling(params.fort)} />
      }>
        <Show when={!isAuthRequired()} fallback={
          <SignInForm fort={params.fort} onComplete={() => startPolling(params.fort)} />
        }>
          <ShellLayout sidebar={sidebarComponent()}>{props.children}</ShellLayout>
        </Show>
      </Show>
    </FortShellContext.Provider>
  );
};
```

Priority order: setup mode → sign-in → normal view.

**Step 4: Commit**

```bash
git add web/shell/src/components/sign-in-form.tsx web/shell/src/app.tsx web/shell/src/stores/services.ts
git commit -m "feat: add sign-in view for returning users"
```

---

### Task 6: Sharkfin — Handle Auth Failure Gracefully

**Repo:** `sharkfin/lead`

**Files:**
- Modify: `web/src/components/chat.tsx`

**Step 1: Fix the infinite skeleton issue**

Currently when `initApp()` fails, `loading()` stays `true` forever. Fix `chat.tsx` so it detects the failure and shows appropriate UI:

```tsx
export function SharkfinChat(props: SharkfinChatProps) {
  const [initFailed, setInitFailed] = createSignal(false);

  onMount(async () => {
    try {
      await initApp();
      const disposeIdle = useIdleDetection(getClient());
      onCleanup(disposeIdle);
    } catch {
      setInitFailed(true);
    }
  });

  // Retry when connected flips to true (shell re-polled after auth).
  createEffect(() => {
    if (props.connected && initFailed()) {
      setInitFailed(false);
      initApp()
        .then(() => {
          const disposeIdle = useIdleDetection(getClient());
          onCleanup(disposeIdle);
        })
        .catch(() => setInitFailed(true));
    }
  });

  return (
    <div class="sf-main">
      <Show when={initFailed()}>
        <wf-banner variant="info" headline="Sign in to use Chat" />
      </Show>
      <Show when={!initFailed() && loading()}>
        <div style="padding: var(--wf-space-lg);">
          <wf-skeleton width="100%" height="2rem" />
          <wf-skeleton width="100%" height="200px" style="margin-top: var(--wf-space-md);" />
          <wf-skeleton width="60%" height="1rem" style="margin-top: var(--wf-space-md);" />
        </div>
      </Show>
      <Show when={!initFailed() && !loading() && connectionState() !== 'connecting'}>
        <Show when={connectionState() === 'disconnected'}>
          <wf-banner variant="warning" headline="Connection lost. Reconnecting\u2026" />
        </Show>
        <ChatContent />
      </Show>
    </div>
  );
}
```

Key changes:
- `initFailed` signal tracks whether `initApp()` threw
- When `initFailed` is true, shows "Sign in to use Chat" instead of infinite skeletons
- When `connected` prop flips to `true` (shell successfully authed and services reconnected), retries `initApp()`

**Step 2: Run tests**

Run: `cd sharkfin/lead/web && pnpm test`
Expected: All tests pass. The chat test mocks `initApp` to succeed, so it's unaffected.

**Step 3: Commit**

```bash
git add web/src/components/chat.tsx
git commit -m "fix: handle auth failure gracefully — show sign-in prompt instead of infinite skeletons"
```

---

### Task 7: Integration Verification

After all tasks are complete:

1. Start Passport (Nexus VM with the updated image)
2. Start Sharkfin daemon with `--passport-url http://passport.nexus:3000`
3. Start shell Vite dev server + BFF
4. Open browser to the BFF

**Expected flow:**
- Shell loads, polls services, detects `setup_mode: true` on auth service
- Setup form appears (email, username, password, confirm)
- User creates admin account → sign-up succeeds → sign-in succeeds → cookie set
- Shell re-polls → `setup_mode` gone → normal view loads
- Click Chat → Sharkfin MF remote loads → `initApp()` connects WS (BFF converts cookie to JWT) → chat UI renders

**Verify with Playwright MCP:**
1. Navigate to BFF URL
2. Snapshot — should show setup form
3. Fill form and submit
4. Snapshot — should show normal shell with services
5. Click Chat — should show chat UI (not skeletons, not "unavailable")
