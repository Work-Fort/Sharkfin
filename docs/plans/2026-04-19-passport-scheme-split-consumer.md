---
type: plan
step: "1"
title: "Passport scheme split — Sharkfin consumer migration"
status: approved
assessment_status: complete
provenance:
  source: cross-repo-coordination
  issue_id: "Cluster 3b (Passport, 2026-04-19)"
  roadmap_step: null
dates:
  created: "2026-04-19"
  approved: "2026-04-19"
  completed: null
related_plans:
  - passport/lead/docs/plans/2026-04-19-auth-scheme-dispatch.md
  - hive/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
  - flow/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
  - pylon/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
  - combine/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
---

# Sharkfin — Passport Scheme Split Consumer Migration

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update Sharkfin's Go client and the `mcp-bridge` subcommand to send API keys under the new `Authorization: ApiKey-v1 <key>` scheme instead of `Authorization: Bearer <key>`. Outbound consumer clients are API-key-only — drop the `WithToken` (JWT) outbound option entirely. Update test fixtures (the e2e harness JWKS stub) so the new scheme is exercised end-to-end.

**Background / Why:** Per TPM clarification 2026-04-19: only web browser clients use JWT; agents and services use API keys. Outbound consumer clients are therefore API-key-only. Sharkfin's client today exposes both `WithToken(jwt)` and `WithAPIKey(key)`; the `WithToken` option is what enabled Flow's latent-bug (Flow was passing a `wf-svc_*` API key into `WithToken`, which prefixed it with `Bearer` and got accidentally accepted via passport's validator-fallthrough). Removing/renaming the JWT-side option makes that misuse impossible by construction. Inbound middleware in Sharkfin still needs both schemes (browser-routed JWT via Scope; ApiKey-v1 from agents) — that path is untouched here.

**Architecture:** Two outbound regions:

1. **`client/go`** — drop `WithToken` entirely. The remaining `WithAPIKey` becomes the only auth option, and the underlying transport sends `ApiKey-v1`. Both the REST transport (`rest_transport.go`) and the WS Dial + reconnect (`client.go`) lose their `Bearer` branch.
2. **`cmd/mcpbridge/mcp_bridge.go`** — the bridge already uses `--api-key` exclusively, so all three call sites (`startPresence` WS dial, `processStdin` POST loop, `callUnreadMessages` POST) flip from `Bearer` to `ApiKey-v1`.

The e2e harness `jwks_stub.go` keeps `signJWT` for inbound-middleware tests (still needed for browser-routed JWT acceptance); only the documenting comment changes.

**Tech Stack:** Go 1.x, `gorilla/websocket`, `net/http`. Pin `service-auth` to the local passport branch via `replace` for verification, then bump to the released version when passport's branch lands.

---

## Conventions

- All commits use Conventional Commits multi-line + `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>` trailer.
- Run unit tests: `mise run test` (from `sharkfin/lead`).
- Run e2e suite: `mise run e2e` (this is the load-bearing verification).
- Lint: `mise run lint`.
- **Do not push** until passport's plan is ready to push too — see the parent plan's "Coordination" section.

---

## Pre-flight: pin to local passport branch

**Files:**
- Modify: `client/go/go.mod` (add `replace`) — **only if it directly imports `service-auth`**; the client today does not (verify with `grep service-auth client/go/go.mod` — expected: no match).
- Modify: root `go.mod` (add `replace`) — the daemon and harness consume `service-auth` here.

`client/go/` is a separate Go module (`module github.com/Work-Fort/sharkfin/client/go`). Each module needs its own replace directive — and each must run `go mod tidy` from its own directory. Only add the replace where `service-auth` is an actual dependency.

Add at the bottom of each affected `go.mod`:

```
replace github.com/Work-Fort/Passport/go/service-auth => /home/kazw/Work/WorkFort/passport/lead/go/service-auth
```

Run `go mod tidy` separately in each affected module's directory:

```bash
cd /home/kazw/Work/WorkFort/sharkfin/lead && go mod tidy
# Only if client/go/go.mod has a service-auth dep:
# cd /home/kazw/Work/WorkFort/sharkfin/lead/client/go && go mod tidy
```

This lets us iterate against passport's pre-release scheme-dispatch branch. The replace is removed before push (Task 5).

**Commit:**

```bash
git commit -m "$(cat <<'EOF'
chore: pin passport service-auth to local branch for scheme-split work

Temporary replace directive — removed before push. Lets us verify
the ApiKey-v1 scheme migration against passport's pre-release branch
before either repo's commits go to the remote.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 1: Enumerate every `WithToken` caller and `Bearer ` test assertion

**Files:**
- Read-only inventory pass; produce a list before any code edits.

**Step 1: Run the enumeration greps and snapshot the results**

```bash
cd /home/kazw/Work/WorkFort/sharkfin/lead

# Every WithToken use (must all migrate, or the package won't compile after Task 2):
grep -rn 'WithToken' client/go/ cmd/ --include='*.go'

# Every Bearer-with-API-key-shaped-literal assertion in tests (must flip to ApiKey-v1):
grep -rn 'Bearer.*\(sk-\|wf-svc_\|wf-agent_\|test-api-key\|tok\|test-token\|my-jwt-token\)' --include='*_test.go' client/go/ cmd/

# Operator-facing callers:
grep -rn 'WithToken\|WithAPIKey' doc.go cmd/ --include='*.go'
```

**Step 2: Snapshot the expected results in this plan (verified at planning time)**

Snapshot at planning time (re-run before editing — files may drift):

`WithToken` callers (every one of these must migrate to `WithAPIKey`, or the package will not compile after Task 2 deletes `WithToken`):

| File | Lines |
| --- | --- |
| `client/go/client.go` | 70-73 (definition itself — delete in Task 2) |
| `client/go/client_test.go` | 758, 820, 852, 885, 917 (5 callers) |
| `client/go/rest_client_test.go` | 19, 44, 68, 103, 122, 152, 184, 221, 246, 274, 295, 315, 345, 433 (14 callers) |
| `client/go/doc.go` | 20, 34 (godoc examples) |
| `client/go/rest_client.go` | 24 (godoc comment) |

`Bearer <api-key-literal>` test assertions to flip:

| File | Line | Currently | Becomes |
| --- | --- | --- | --- |
| `client/go/client_test.go` | 764-765 | `Bearer my-jwt-token` | DELETE — `WithToken` no longer exists |
| `client/go/client_test.go` | 793-794 | `Bearer sk-test-key` | `ApiKey-v1 sk-test-key` |
| `client/go/rest_client_test.go` | 36-37 | `Bearer tok` | `ApiKey-v1 tok` |

If the planning-time list differs from the live grep, treat the live grep as authoritative and migrate every additional hit. The plan is wrong, not the source.

**Step 3: No commit — this task produces only the inventory.**

---

### Task 2: Drop `WithToken`, send API keys under `ApiKey-v1` in `rest_transport.go` + options

**Files:**
- Modify: `client/go/client.go` (lines 70-89 — delete `WithToken`, keep `WithAPIKey`; `Option` and `options` struct live here, not in a separate `options.go`)
- Modify: `client/go/rest_transport.go` (drop the `t.token` branch and field; flip `apiKey` branch to `ApiKey-v1`)
- Modify: `client/go/doc.go` (godoc examples on lines 20, 34 — change `client.WithToken(tok)` → `client.WithAPIKey(tok)`)
- Modify: `client/go/rest_client.go` (godoc on line 24 — drop the "WithToken or" phrasing)
- Test: `client/go/rest_client_test.go`

**Step 1: Add a failing test for the API-key path**

Add to `client/go/rest_client_test.go` (this test is in addition to flipping the existing assertions; it asserts the API-key wire format end-to-end on the REST transport):

```go
func TestRESTClient_APIKeyUsesApiKeyV1Scheme(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithAPIKey("wf-svc_secret"))
	_, _ = c.Channels(context.Background())

	if gotAuth != "ApiKey-v1 wf-svc_secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "ApiKey-v1 wf-svc_secret")
	}
}
```

(`Channels(ctx)` is the canonical zero-arg GET on `RESTClient` — already used at `rest_client_test.go:69` against the test server fixture. If the live source has a different method available with the same shape, use that.)

**Step 2: Flip every existing `Bearer <api-key-literal>` test assertion**

Apply the table from Task 1, Step 2 mechanically:

- `client/go/rest_client_test.go:36-37` — change `"Bearer tok"` → `"ApiKey-v1 tok"`. The error message at line 37 also flips.
- `client/go/client_test.go:793-794` — change `"Bearer sk-test-key"` → `"ApiKey-v1 sk-test-key"` and the error message.
- `client/go/client_test.go:758-765` — DELETE the entire test function (the `WithToken("my-jwt-token")` test). `WithToken` no longer exists.

**Step 3: Migrate every `WithToken` caller to `WithAPIKey` (mechanical rename)**

Apply to all 14 occurrences in `rest_client_test.go` (lines 19, 44, 68, 103, 122, 152, 184, 221, 246, 274, 295, 315, 345, 433) and the 4 remaining occurrences in `client_test.go` (lines 820, 852, 885, 917 — line 758's caller was deleted in Step 2). Each is a 1:1 swap: `WithToken("tok")` → `WithAPIKey("tok")`. The literal `"tok"` is a test value standing in for an API key; no rename of the literal is required, only the option function name.

`client/go/doc.go` lines 20 and 34 also rename `client.WithToken(tok)` → `client.WithAPIKey(tok)`.

`client/go/rest_client.go:24` — change "Authentication is provided via WithToken or WithAPIKey" to "Authentication is provided via WithAPIKey".

**Step 4: Run and confirm failure on the new test (before any production-code change)**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead && mise run test -- -run TestRESTClient_APIKeyUsesApiKeyV1Scheme ./client/go/...
```

Expected: FAIL — `Authorization = "Bearer wf-svc_secret"`.

**Step 5: Drop `WithToken` and update the transport**

In `client/go/client.go`, delete the `WithToken` function block (lines 70-73 in the snapshot — the four lines starting `// WithToken sets…` and ending at the closing `}`). Also remove the `token` field from the unexported `options` struct (search for `token` field declaration in the same file — if `WithToken` was the only writer, the field is now dead). Update the godoc on `Option` (line 89 in the snapshot — "Authentication is provided via WithToken or WithAPIKey options.") to drop the `WithToken or` phrasing.

In `client/go/rest_transport.go`, replace:

```go
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	} else if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
```

with:

```go
	if t.apiKey != "" {
		req.Header.Set("Authorization", "ApiKey-v1 "+t.apiKey)
	}
```

Remove the `t.token` field declaration from the transport struct (around line 21 in the snapshot).

**Step 6: Verify**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead && mise run test -- -run TestRESTClient ./client/go/...
```

Expected: all `TestRESTClient_*` tests PASS, including the new `TestRESTClient_APIKeyUsesApiKeyV1Scheme` and the flipped `Bearer tok` → `ApiKey-v1 tok` assertions.

**Step 7: Commit**

```bash
git add client/go/client.go client/go/rest_transport.go client/go/rest_client.go client/go/doc.go client/go/rest_client_test.go client/go/client_test.go
git commit -m "$(cat <<'EOF'
feat(client)!: drop WithToken; API keys travel under ApiKey-v1

BREAKING CHANGE: WithToken is removed. Outbound consumer clients are
API-key-only (per TPM clarification 2026-04-19: only web browser
clients use JWT; agents and services use API keys). The remaining
WithAPIKey option now emits "ApiKey-v1 <key>" instead of "Bearer <key>",
aligning with passport's scheme-dispatch middleware.

Every WithToken caller in tests (rest_client_test.go ×14,
client_test.go ×4) is migrated to WithAPIKey; the JWT-side test
(client_test.go:758-765 in the pre-edit snapshot) is deleted because
the option no longer exists. The corresponding Bearer-with-API-key
assertions (rest_client_test.go:36-37, client_test.go:793-794) flip
to ApiKey-v1.

Removing WithToken closes the latent-bug class where callers passed
a wf-svc_* API key into WithToken and accidentally got accepted via
passport's validator fallthrough.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Update `client/go/client.go` Dial + reconnect headers

**Files:**
- Modify: `client/go/client.go:98-103` (Dial header construction — drop the `o.token` branch, flip `o.apiKey` to `ApiKey-v1`)
- Modify: `client/go/client.go:208-213` (reconnectLoop header construction — same pattern, second call site)
- (The `client_test.go:758-765` deletion and `client_test.go:793-794` flip already happened in Task 2 alongside the `WithToken` removal — Task 3 is purely the production-code edit.)

**Step 1: Run the impacted tests to confirm they FAIL after Task 2 deleted the JWT-side option but BEFORE the production-code flip lands**

```
mise run test -- -run TestClient_ -v ./client/go/...
```

Expected: the test asserting `ApiKey-v1 sk-test-key` (renamed in Task 2) FAILs with `Authorization = "Bearer sk-test-key"` because `client.go` Dial + reconnect still call `Bearer + o.apiKey`. This is the failing-test gate for Task 3.

**Step 2: Apply the implementation change**

In `client/go/client.go`, in **both** the `Dial` header block (around line 98) and the `reconnectLoop` header block (around line 208), replace:

```go
	if o.token != "" {
		header.Set("Authorization", "Bearer "+o.token)
	} else if o.apiKey != "" {
		header.Set("Authorization", "Bearer "+o.apiKey)
	}
```

with:

```go
	if o.apiKey != "" {
		header.Set("Authorization", "ApiKey-v1 "+o.apiKey)
	}
```

Same edit in the `c.opts.token` / `c.opts.apiKey` reconnect block (line ~211-213).

**Step 3: Verify**

```
mise run test -- -run TestClient_ -v ./client/go/...
```

Expected: PASS.

**Step 4: Commit**

```bash
git add client/go/client.go
git commit -m "$(cat <<'EOF'
feat(client)!: WS Dial + reconnect drop JWT path; API keys as ApiKey-v1

BREAKING CHANGE: WebSocket dial and reconnect headers no longer carry
the JWT (WithToken) branch — that option was removed in the previous
commit. The remaining API-key path now sends ApiKey-v1 instead of
Bearer, matching the REST transport. Both Dial() and the internal
reconnectLoop are updated.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Update `cmd/mcpbridge/mcp_bridge.go` (3 call sites)

**Files:**
- Modify: `cmd/mcpbridge/mcp_bridge.go:79` (presence WS dial)
- Modify: `cmd/mcpbridge/mcp_bridge.go:166` (processStdin POST)
- Modify: `cmd/mcpbridge/mcp_bridge.go:282` (callUnreadMessages POST)

The bridge uses an `--api-key` flag exclusively, so all three call sites send API keys.

**Step 1: Edit each call site**

Replace each:

```go
	header.Set("Authorization", "Bearer "+b.apiKey)
```

(or `req.Header.Set` / `httpReq.Header.Set` variant) with:

```go
	header.Set("Authorization", "ApiKey-v1 "+b.apiKey)
```

(matching the original setter type — `header` for the WS dial, `req` for the POST loop, `httpReq` for the unread call).

**Step 2: Verify the bridge still builds**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead && mise run build:dev
```

Expected: build OK.

**Step 3: Run the bridge-level e2e (process leak test pulls the binary in)**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead && mise run e2e -- -run TestProcessLeak ./tests/e2e/harness/...
```

Expected: PASS.

**Step 4: Commit**

```bash
git add cmd/mcpbridge/mcp_bridge.go
git commit -m "$(cat <<'EOF'
feat(mcpbridge)!: send API key as ApiKey-v1 on all three outbound paths

BREAKING CHANGE: the mcp-bridge subcommand sends its --api-key under
the ApiKey-v1 Authorization scheme on:
- the /presence WebSocket upgrade
- the /mcp POST loop in processStdin
- the /mcp POST in callUnreadMessages

Required by passport's scheme-dispatch middleware (Bearer is now
JWT-only).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Update the e2e harness JWKS stub to expect `ApiKey-v1`

**Files:**
- Modify: `tests/e2e/harness/jwks_stub.go` (the comment block; the `signJWT` helper itself stays — it's used by inbound-middleware tests that exercise the JWT-acceptance path, which is still needed for browser-routed traffic)

**Step 1: Update the documenting comment**

In `tests/e2e/harness/jwks_stub.go`, the comment block above `validBridgeAPIKey` (lines 18-23) currently says "an invalid token that falls through the JWT validator into the API-key validator". After the scheme split, that fallthrough cannot happen. Rewrite the comment:

```go
// validBridgeAPIKey is the only API key accepted by the JWKS stub's
// verify-api-key endpoint. Tests that start a bridge use this same key
// (see harness.StartBridge callers passing "test-api-key"). Returning
// the bridge identity for any non-empty key would make a real
// production bug — a stolen API key being sent under the wrong scheme
// — silently pass in tests. The stub mirrors production: only this
// exact key resolves to the bridge identity.
//
// As of 2026-04-19, passport's middleware also dispatches by
// Authorization scheme (Bearer → JWT only, ApiKey-v1 → this endpoint
// only), so the historical "invalid JWT falls through to verify-api-key"
// concern documented here previously is impossible by construction.
//
// signJWT below remains in use: inbound-middleware tests still need to
// exercise the JWT-acceptance path (browser-routed traffic). Only the
// outbound JWT-sending was removed from consumer clients.
const validBridgeAPIKey = "test-api-key"
```

**Step 2: Add an e2e regression-prevention assertion that `Bearer wf-svc_*` is rejected by the inbound middleware**

Add a new test in the same package that drives the daemon end-to-end (the harness already starts a sharkfin daemon with the JWKS stub). The test sends a literal API-key string under the wrong scheme and asserts 401 + zero `verify-api-key` calls:

```go
func TestInboundMiddleware_BearerForAPIKeyReturns401(t *testing.T) {
	srv := harness.Start(t) // canonical sharkfin daemon harness
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/channels", nil)
	req.Header.Set("Authorization", "Bearer "+harness.ValidBridgeAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (API key sent under Bearer must not be accepted)", resp.StatusCode)
	}
	if got := srv.JWKS.APIKeyVerifyCount(); got != 0 {
		t.Errorf("APIKeyVerifyCount = %d, want 0 (Bearer must not fall through to verify-api-key)", got)
	}
}
```

(Adapt to whatever the canonical harness entry point is — the assessor's read of `tests/e2e/harness/` confirms `JWKSStub.APIKeyVerifyCount()` already exists. If the harness exposes a different test-server constructor, use that — the load-bearing assertions are the 401 status and the zero call count.)

**Step 3: Run the full e2e suite**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead && mise run e2e
```

Expected: all 65/66 baseline PASS plus the new `TestInboundMiddleware_BearerForAPIKeyReturns401` PASS.

**Step 4: Commit**

```bash
git add tests/e2e/harness/jwks_stub.go tests/e2e/
git commit -m "$(cat <<'EOF'
test(e2e): document scheme-dispatch invariant + assert Bearer-for-APIkey 401

Passport's middleware no longer falls through from JWT to API-key on
parse failure (post-2026-04-19 scheme split), so the historical
concern about silently authenticating malformed JWTs is impossible by
construction. Update the doc comment on validBridgeAPIKey to reflect
the new invariant; signJWT itself stays for inbound-middleware tests.

Adds a regression-prevention e2e test that POSTs a valid API-key
string under "Bearer" and asserts 401 + zero verify-api-key calls.
This proves the inbound middleware now rejects the wrong scheme even
for a string the verify-api-key endpoint would accept under
ApiKey-v1 — the load-bearing closure of Cluster 3b at the daemon
boundary.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Drop the local `replace`, bump the dependency, retest, commit

**Files:**
- Modify: root `go.mod` (remove `replace`, run `go get`)
- Modify: `client/go/go.mod` ONLY if it directly depends on `service-auth` (verify with `grep service-auth client/go/go.mod` — at planning time, no match)

**Sequencing:** This task only runs after passport's plan has been pushed AND the new `service-auth` version has been tagged by the semver-tagging-action (per the team's `feedback_push_consumer_clients.md`). The Team Lead must confirm the tag exists before this task starts.

**Step 1: Remove the replace directive(s)**

Delete the `replace github.com/Work-Fort/Passport/go/service-auth => …` line from each `go.mod` that has one. Per the pre-flight, this is the root `go.mod` only unless the client module gained a direct dep in the meantime.

**Step 2: Bump to the released version**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead
go get github.com/Work-Fort/Passport/go/service-auth@<new-tag>
go mod tidy
# Only if the client module has a direct dep:
# cd client/go && go get github.com/Work-Fort/Passport/go/service-auth@<new-tag> && go mod tidy
```

**Step 3: Run all tests**

```
cd /home/kazw/Work/WorkFort/sharkfin/lead && mise run lint && mise run test && mise run e2e
```

Expected: all PASS.

**Step 4: Commit and push**

```bash
git add go.mod go.sum
# If client/go.{mod,sum} also changed, stage them too:
git add client/go/go.mod client/go/go.sum 2>/dev/null || true
git commit -m "$(cat <<'EOF'
chore(deps): bump passport service-auth to <new-tag> (scheme dispatch)

Drops the local replace directive used during the scheme-split work
and pins to the released service-auth tag that ships the new
NewSchemeDispatch middleware. Sharkfin's client + mcp-bridge already
send API keys as ApiKey-v1 (previous commits in this branch); the
JWT-side WithToken option was removed.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

Push triggers the `client/go` semver tag (per repo convention).

---

## Verification checklist (before declaring done)

- [ ] `mise run lint` clean
- [ ] `mise run test` 100% green
- [ ] `mise run e2e` 65/66 PASS (1 intentional skip baseline)
- [ ] `WithToken` no longer exists in `client/go/options.go`
- [ ] No `Bearer ` prefix anywhere in `client/go/` or `cmd/mcpbridge/` (`grep -rn 'Bearer' client/go/ cmd/mcpbridge/`)
- [ ] `go.mod` has no `replace` directive pointing at a local path
- [ ] `client/go/<tag>` exists on the remote after push (semver-tagging-action)
