---
type: plan
step: "1"
title: "sharkfin e2e harness — orphan-leak hardening"
status: pending
assessment_status: complete
provenance:
  source: roadmap
  issue_id: null
  roadmap_step: null
dates:
  created: "2026-04-19"
  approved: null
  completed: null
related_plans: []
---

# Sharkfin E2E Harness — Orphan-Leak Hardening

**Goal:** Stop the e2e harness from leaking orphan processes. This is
the bug class that hung Sharkfin's CI for ~55 minutes after the recent
push — the daemon spawn at `tests/e2e/harness/harness.go:99-112` wires
`cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)`, which makes
`exec.Cmd` create an OS pipe and a copy goroutine. Any descendant or
late-write that holds the pipe's read end keeps `cmd.Wait()` blocked
until the workflow timeout fires. Sharkfin also has two daemon-cleanup
paths (`Stop`, `StopNoClean`) that need identical treatment, plus a
`Bridge` subprocess (line 413-446) whose `Stop` (line 467) has no
SIGKILL fallback at all — a bridge that ignores SIGTERM hangs the
harness indefinitely. Fixing the bridge in the same pass closes the
second leak.

**Canonical fix** (see `/home/kazw/Work/WorkFort/skills/lead/go-service-architecture/references/architecture-reference.md` — section
"Orphan-Process Hardening (Required)"):

1. **`Setpgid: true`** in `cmd.SysProcAttr`.
2. **`*os.File` for stdout/stderr**, not `io.MultiWriter`.
3. **Negative-pid kill** (`syscall.Kill(-pgid, sig)`).
4. **`cmd.WaitDelay = 10 * time.Second`** safety net.

All four parts are load-bearing. Apply the same pattern to the bridge
spawn — the bridge inherits `os.Stderr` directly today (line 433),
which avoids the pipe-goroutine class but still leaves no group-kill
defence against descendants.

**Repo specifics.** Sharkfin's harness has more surface than the other
five:
- Two daemon-stop variants (`Stop` line 213 and `StopNoClean` line
  155) — both need the four-part fix and must share the kill
  mechanism. Extract a small `stopDaemonProcess` helper to avoid
  drift.
- `Cleanup` (line 179) handles tempdir removal separately from the
  daemon process — it stays unchanged.
- `GrantAdmin` (line 184) shells out to a CLI subprocess that runs
  to completion synchronously — no leak risk there, no changes.
- `Bridge.Stop` (line 467) needs the SIGKILL fallback added; today
  it just signals SIGTERM and waits forever. Apply the same Setpgid
  + WaitDelay + group-kill on the bridge spawn for parity.
- `WSClient`, `PresenceClient`, `Client` are network clients, not
  subprocesses — out of scope.

**Tech stack:** Go 1.25.0 (e2e nested module), `os/exec`, `syscall`.
No new dependencies.

**Commands:** `mise run e2e` (the existing task at `.mise/tasks/e2e`)
runs `mise run build:dev` then `cd tests/e2e && go test -v -race
-timeout 180s`. Targeted runs use `cd tests/e2e && go test -run TestX
-count=1 ./harness/...`.

---

## Prerequisites

- `tests/e2e/go.mod` (Go 1.25.0) — `cmd.WaitDelay` (Go 1.20+) is
  available.
- `mise run build:dev` builds the sharkfin binary used by both the
  daemon and the bridge spawn.

---

## Conventions

- Run all build/test commands via `mise run <task>` from
  `sharkfin/lead/`. Targeted go test runs are permitted from inside
  `tests/e2e/`.
- Commit after each task with the multi-line conventional-commits
  HEREDOC and the Co-Authored-By trailer below.

```bash
git add <files>
git commit -m "$(cat <<'EOF'
<type>(<scope>): <description>

<body explaining why, not what>

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task Breakdown

### Task 1: Write failing leak-detection tests for daemon and bridge

**Files:**
- Create: `tests/e2e/harness/process_leak_test.go`

**Step 1: Write the tests**

Two tests: one covers `StartDaemon` + `Stop`, the other covers
`StartBridge` + `Bridge.Stop`. Both assert the spawned process lives
in its own group and that group is empty after stop.

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package harness

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDaemonStop_KillsProcessGroup(t *testing.T) {
	binary := os.Getenv("SHARKFIN_BINARY")
	if binary == "" {
		t.Skip("SHARKFIN_BINARY not set; run via 'mise run e2e'")
	}

	addr, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}

	d, err := StartDaemon(binary, addr)
	if err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	pid := d.cmd.Process.Pid

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	if pgid != pid {
		t.Fatalf("daemon pgid = %d, want %d (Setpgid not set)", pgid, pid)
	}
	// Defence against the (vanishingly rare) case where the test
	// process itself is in a group whose id equals the daemon PID.
	if pgid == os.Getpid() {
		t.Fatalf("daemon pgid (%d) equals harness pid; daemon inherited harness group", pgid)
	}

	// Stop returns (stderrBytes, err); we ignore the bytes here.
	if _, err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Use errors.Is (not direct ==) because syscall.Errno implements
	// the errors.Is contract and errors.Is is the idiomatic Go choice.
	if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kill(-%d, 0) = %v, want ESRCH (group still has live members)", pgid, err)
	}
}

func TestBridgeStop_KillsProcessGroup(t *testing.T) {
	binary := os.Getenv("SHARKFIN_BINARY")
	if binary == "" {
		t.Skip("SHARKFIN_BINARY not set; run via 'mise run e2e'")
	}

	// Bridge needs an addressable daemon — boot one for the test.
	addr, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	d, err := StartDaemon(binary, addr)
	if err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	defer d.Stop()

	xdgDir := filepath.Join(t.TempDir(), "bridge")
	b, err := StartBridge(binary, addr, xdgDir, "test-api-key")
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	pid := b.cmd.Process.Pid

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	if pgid != pid {
		t.Fatalf("bridge pgid = %d, want %d (Setpgid not set)", pgid, pid)
	}
	if pgid == os.Getpid() {
		t.Fatalf("bridge pgid (%d) equals harness pid; bridge inherited harness group", pgid)
	}

	if err := b.Stop(); err != nil {
		// SIGKILL exit may surface here once the fallback lands; the
		// pgid emptiness check below is the load-bearing assertion.
		t.Logf("Bridge.Stop returned: %v", err)
	}

	if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kill(-%d, 0) = %v, want ESRCH (group still has live members)", pgid, err)
	}
}
```

**Step 2: Run the tests to verify they fail**

```
cd sharkfin/lead && go build -o /tmp/sharkfin ./
SHARKFIN_BINARY=/tmp/sharkfin go test \
  -run "TestDaemonStop_KillsProcessGroup|TestBridgeStop_KillsProcessGroup" \
  -count=1 ./tests/e2e/harness/...
```

Expected: both FAIL with `daemon pgid = ... want ... (Setpgid not
set)` or `bridge pgid = ... want ... (Setpgid not set)`.

Both tests stay red after Task 1. The daemon test passes after Task
2; the bridge test stays failing until Task 3 (which adds Setpgid
to the bridge spawn).

**Step 3: Commit the failing tests**

```bash
git add tests/e2e/harness/process_leak_test.go
git commit -m "$(cat <<'EOF'
test(e2e): add failing leak tests for daemon and bridge

Asserts that StartDaemon and StartBridge both place their spawned
processes into their own process groups and that Stop empties each
group. Currently fails because neither spawn sets Setpgid; the next
two tasks fix the daemon and bridge harnesses respectively.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Apply the four-part canonical fix to daemon spawn and both Stop variants

**Depends on:** Task 1

**Files:**
- Modify: `tests/e2e/harness/harness.go` — `Daemon` struct (lines
  48-55), `StartDaemon` (lines 57-136), `StopNoClean` (lines 155-176),
  `Stop` (lines 213-233). Adds a `stopDaemonProcess` helper.

**Step 1: Update imports**

`io` is no longer needed once we stop using `io.MultiWriter`. Add
`syscall`. Final block:

```go
import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"
)
```

**Step 2: Replace the `Daemon` struct's stderr field**

The current struct (lines 48-55) holds `stderr *bytes.Buffer`. Swap
for `stderrFile *os.File`:

```go
type Daemon struct {
	cmd        *exec.Cmd
	addr       string
	xdgDir     string
	stderrFile *os.File // *os.File (not bytes.Buffer) — see hardening notes
	stubStop   func()
	signJWT    func(id, username, displayName, userType string) string
}
```

**Step 3: Rewrite the spawn block in `StartDaemon`**

Replace lines 97-112 (from `var stderrBuf bytes.Buffer` through the
`cmd.Start()` block) with:

```go
	stderrFile, err := os.CreateTemp("", "sharkfin-e2e-stderr-*")
	if err != nil {
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("create stderr temp file: %w", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
		fmt.Sprintf("SHARKFIN_PRESENCE_TIMEOUT=%s", cfg.presenceTimeout),
	)
	// *os.File (not io.MultiWriter) so exec.Cmd does not create a
	// copy goroutine; Setpgid puts the daemon and any descendants
	// in a fresh process group; WaitDelay force-closes any inherited
	// fds after the daemon exits. See the orphan-process hardening
	// section of go-service-architecture.
	cmd.Stdout = stderrFile
	cmd.Stderr = stderrFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second

	if err := cmd.Start(); err != nil {
		stderrFile.Close()
		os.Remove(stderrFile.Name())
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("start daemon: %w", err)
	}
```

**Step 4: Update the readiness loop's success and failure paths**

The success literal at lines 119-126 needs the new field:

```go
		if err == nil {
			conn.Close()
			return &Daemon{
				cmd:        cmd,
				addr:       addr,
				xdgDir:     xdgDir,
				stderrFile: stderrFile,
				stubStop:   stubStop,
				signJWT:    signJWT,
			}, nil
		}
```

The not-ready failure path (lines 131-135) needs the group-kill plus
file cleanup:

```go
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	cmd.Wait()
	stderrFile.Close()
	os.Remove(stderrFile.Name())
	os.RemoveAll(xdgDir)
	stubStop()
	return nil, fmt.Errorf("daemon did not become ready on %s", addr)
```

**Step 5: Add a `stopDaemonProcess` helper**

Both `Stop` and `StopNoClean` need to send SIGTERM-then-SIGKILL to
the process group and read the captured stderr. Extract:

```go
// stopDaemonProcess signals the daemon's process group with SIGTERM,
// waits up to 5s, then SIGKILLs the group. Returns the wait error
// (if any) and the captured stdout+stderr bytes for DATA RACE
// detection. Safe to call when d.cmd.Process is already nil.
func (d *Daemon) stopDaemonProcess() (waitErr error, stderrBytes []byte) {
	if d.cmd.Process != nil {
		pgid := d.cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- d.cmd.Wait() }()
		select {
		case waitErr = <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
			waitErr = fmt.Errorf("daemon did not exit after SIGTERM")
		}
	}
	if d.stderrFile != nil {
		stderrBytes, _ = os.ReadFile(d.stderrFile.Name())
		d.stderrFile.Close()
		os.Remove(d.stderrFile.Name())
		d.stderrFile = nil
	}
	return waitErr, stderrBytes
}
```

**Step 6: Rewrite `StopNoClean`**

Replace `StopNoClean` (lines 155-176) with:

```go
// StopNoClean stops the daemon and JWKS stub without removing the xdg
// directory. Use Cleanup() later to remove it.
func (d *Daemon) StopNoClean(t testing.TB) {
	t.Helper()
	if d.stubStop != nil {
		d.stubStop()
	}
	waitErr, stderrBytes := d.stopDaemonProcess()
	if waitErr != nil {
		t.Log(waitErr)
	}
	if bytes.Contains(stderrBytes, []byte("DATA RACE")) {
		t.Fatalf("data race detected in daemon stderr:\n%s", stderrBytes)
	}
}
```

**Step 7: Rewrite `Stop` to return captured stderr**

`stopDaemonProcess` already returns `(waitErr, stderrBytes)`. Route
those bytes through `Stop` so callers (including `StopFatal`) see the
daemon's full output AFTER `cmd.Wait` — including anything written
between SIGTERM and exit. That's exactly where DATA RACE markers from
teardown panics, defer-ordering issues between goroutines, and
controller-shutdown races appear; a snapshot-before-Stop pattern
would miss them.

Replace `Stop` (lines 213-233) with:

```go
// Stop tears down the JWKS stub, signals the daemon group, and
// removes the xdg dir. Returns the captured stdout+stderr bytes
// (read after cmd.Wait so teardown writes are included) alongside
// the wait error.
func (d *Daemon) Stop() ([]byte, error) {
	if d.stubStop != nil {
		d.stubStop()
	}
	waitErr, stderrBytes := d.stopDaemonProcess()
	os.RemoveAll(d.xdgDir)
	return stderrBytes, waitErr
}
```

Update existing callers of `d.Stop()` to discard the bytes:
`_, err := d.Stop()`. The change is mechanical; search the test
files (`grep -rn "\.Stop()" tests/e2e/`) and patch each call site.

`StopFatal` (lines 202-211) now uses the bytes returned by `Stop`:

```go
// StopFatal stops the daemon and fails the test if a data race was
// detected.
func (d *Daemon) StopFatal(t testing.TB) {
	t.Helper()
	stderrBytes, err := d.Stop()
	if err != nil {
		t.Logf("daemon stop: %v", err)
	}
	if bytes.Contains(stderrBytes, []byte("DATA RACE")) {
		t.Fatalf("data race detected in daemon stderr:\n%s", stderrBytes)
	}
}
```

This catches DATA RACE markers printed during the daemon's panic
teardown — sharkfin's most-likely race window given its WebSocket
presence, MCP bridge, and async tool-call concurrency.

**Step 8: Run the daemon leak test to verify it passes**

```
SHARKFIN_BINARY=/tmp/sharkfin go test \
  -run TestDaemonStop_KillsProcessGroup \
  -count=1 ./tests/e2e/harness/...
```

Expected: PASS.

**Step 9: Run the full e2e suite to verify no regression**

Run from `sharkfin/lead/`:

```
mise run e2e
```

Expected: PASS. All existing tests still see daemon/MCP/WS/presence
working; the DATA RACE check still runs.

**Step 10: Commit**

```bash
git add tests/e2e/harness/harness.go
git commit -m "$(cat <<'EOF'
fix(e2e): harden daemon harness against orphan-process leaks

Spawn the sharkfin daemon into its own process group (Setpgid),
capture stdout+stderr to an *os.File instead of an io.MultiWriter
(eliminates the copy goroutine that holds pipe fds), signal the
whole group on shutdown (kill(-pgid, ...)), and set WaitDelay so
cmd.Wait force-closes any inherited fd after the daemon exits.
Extract a shared stopDaemonProcess helper so Stop and StopNoClean
share the same termination mechanics.

This is the bug class that hung CI for ~55 minutes on the recent
push: the daemon was alive, but a buffered write held the stderr
pipe, blocking cmd.Wait until the workflow timeout fired. Stop now
returns (stderrBytes, err) so StopFatal sees the daemon's full
output including bytes written between SIGTERM and exit — exactly
where DATA RACE markers from teardown panics surface.

Implements the canonical e2e-harness orphan-leak hardening pattern
documented in skills/lead/go-service-architecture/references/architecture-reference.md
(section "Orphan-Process Hardening (Required)").

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Apply the four-part fix to the Bridge subprocess

**Depends on:** Task 2

**Files:**
- Modify: `tests/e2e/harness/harness.go` — `Bridge` struct (lines
  407-411), `StartBridge` (lines 413-446), `Bridge.Stop` (lines
  467-473).

**Step 1: Rewrite `StartBridge`'s spawn**

The bridge currently inherits `os.Stderr` directly (line 433), which
avoids the pipe-goroutine class — keep that. But we still need
`Setpgid` and `WaitDelay`, plus group-kill on stop. Replace lines
413-446 with:

```go
func StartBridge(binary, daemonAddr, xdgDir, apiKey string) (*Bridge, error) {
	cmd := exec.Command(binary,
		"mcp-bridge",
		"--daemon", daemonAddr,
		"--api-key", apiKey,
		"--log-level", "disabled",
	)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
	)
	// Setpgid + WaitDelay match the daemon spawn — see the orphan-
	// process hardening section of go-service-architecture. Stderr
	// inherits os.Stderr directly (no io.Writer wrapper) so no copy
	// goroutine is created.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start bridge: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	return &Bridge{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdinPipe),
		stdout: bufio.NewScanner(stdoutPipe),
	}, nil
}
```

**Step 2: Rewrite `Bridge.Stop` with group-kill and SIGKILL fallback**

Replace `Stop` (lines 467-473) with:

```go
// Stop signals the bridge's process group with SIGTERM, waits up to
// 5s, then SIGKILLs the group. The previous implementation only
// signalled SIGTERM and waited indefinitely — a bridge that ignored
// the signal hung the harness.
func (b *Bridge) Stop() error {
	if b.cmd.Process == nil {
		return nil
	}
	pgid := b.cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- b.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
		return fmt.Errorf("bridge did not exit after SIGTERM")
	}
}
```

`Bridge.Kill` (line 475-480) only signals the bridge process; update
to group-kill:

```go
func (b *Bridge) Kill() error {
	if b.cmd.Process == nil {
		return nil
	}
	pgid := b.cmd.Process.Pid
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
```

Before applying, verify no test depends on `Bridge.Kill` killing only
the bridge process and not its descendants. Run
`grep -rn "\.Kill()" tests/e2e/` and inspect each call site — the
only caller today is the bridge's own teardown, so the new behaviour
is a strict improvement (descendants get reaped instead of
inheriting init).

**Step 3: Run the bridge leak test to verify it passes**

```
SHARKFIN_BINARY=/tmp/sharkfin go test \
  -run TestBridgeStop_KillsProcessGroup \
  -count=1 ./tests/e2e/harness/...
```

Expected: PASS.

**Step 4: Run the full e2e suite to verify no regression**

```
mise run e2e
```

Expected: PASS. Existing bridge tests (`mcp_bridge` flow) still work
through the new SIGTERM path — a clean shutdown returns within
milliseconds, well under the 5s deadline.

**Step 5: Commit**

```bash
git add tests/e2e/harness/harness.go
git commit -m "$(cat <<'EOF'
fix(e2e): harden bridge harness; add SIGKILL fallback

Set Setpgid on the bridge spawn, signal the whole group on Stop,
and add a 5s SIGTERM-then-SIGKILL deadline so a bridge that
ignores SIGTERM no longer hangs the harness indefinitely.
WaitDelay force-closes any inherited fd after exit. The bridge's
stderr already inherits os.Stderr directly so no pipe-goroutine
fix is needed there.

The previous Bridge.Stop signalled SIGTERM and called cmd.Wait
unbounded — a stuck bridge would block forever.

Implements the canonical e2e-harness orphan-leak hardening pattern
documented in skills/lead/go-service-architecture/references/architecture-reference.md
(section "Orphan-Process Hardening (Required)").

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Verify cleanup is bounded under simulated test failure

**Depends on:** Task 3

**Files:**
- (Temporary, reverted) inject `t.Fatal` into a cheap existing e2e
  test in `tests/e2e/`.

**Step 1: Confirm working tree is clean**

Run `git status`. Expected: clean.

**Step 2: Inject a forced failure**

Pick a small existing test that calls `StartDaemon` (any in
`tests/e2e/`) and add `t.Fatal("synthetic failure to verify cleanup
bound")` immediately after `StartDaemon` returns. Do not commit.
Optionally `git stash push -k -m "synthetic-failure"` then
`git stash pop` so the diff is recoverable.

**Step 3: Time `mise run e2e`**

Run from `sharkfin/lead/`:

```
time mise run e2e
```

Expected:

- The synthetic test FAILs.
- Total wall clock under 30 seconds (typically 10-15s including
  build:dev). The 5s SIGKILL deadline is the worst case per
  process; daemon + bridge teardown is sequential.

If the run exceeds 30 seconds, inspect:

- `ps -o pid,pgid,cmd -p $(pgrep -f sharkfin.*daemon)` — daemon
  surviving means SIGTERM/SIGKILL not delivered.
- `ps -o pid,pgid,cmd -p $(pgrep -f sharkfin.*mcp-bridge)` — bridge
  surviving means same for the bridge.

**Step 4: Revert the synthetic failure**

`git checkout -- <test_file>` to restore. Run `git status` and
confirm the working tree is clean.

**Step 5: Final regression run**

```
mise run e2e
```

Expected: PASS, all tests green.

No commit for this task — verification only.

---

## Verification Checklist

After all tasks complete:

- [ ] `mise run e2e` passes from `sharkfin/lead/`.
- [ ] Both `TestDaemonStop_KillsProcessGroup` and
  `TestBridgeStop_KillsProcessGroup` pass; reverting either
  `Setpgid` line in `harness.go` makes the matching test fail with
  the expected message.
- [ ] `Daemon.stderr` is gone; `Daemon.stderrFile` is `*os.File`.
- [ ] Daemon spawn: `cmd.SysProcAttr.Setpgid == true`,
  `cmd.WaitDelay == 10s`, `cmd.Stdout`/`cmd.Stderr` both `*os.File`.
- [ ] Bridge spawn: `cmd.SysProcAttr.Setpgid == true`,
  `cmd.WaitDelay == 10s`. Stderr stays `os.Stderr` direct.
- [ ] `Stop`, `StopNoClean`, `StopFatal`, `Bridge.Stop`, `Bridge.Kill`
  all use `syscall.Kill(-pgid, sig)`, never
  `cmd.Process.Signal`/`cmd.Process.Kill`.
- [ ] `stopDaemonProcess` helper is the single termination
  mechanism for the daemon (used by both `Stop` and `StopNoClean`).
- [ ] `Stop` returns `([]byte, error)`; `StopFatal` reads the
  returned bytes (post-`cmd.Wait`) for the DATA RACE check, so
  teardown writes are included. `StopNoClean` runs the same check
  via `stopDaemonProcess`.
- [ ] `Bridge.Stop` has a 5s SIGTERM-then-SIGKILL deadline, never
  blocks indefinitely.
- [ ] `time mise run e2e` with an injected `t.Fatal` returns in
  under 30 seconds (Task 4 spot check).

## Out of Scope

- Refactoring `WSClient`, `PresenceClient`, `Client`, or `GrantAdmin`.
  None are subprocesses; none can leak orphans.
- Adding new e2e coverage beyond the leak tests.
- Reworking the daemon's signal handlers. The fix is in the harness;
  the daemon's existing SIGTERM behaviour is unchanged.
- Bridge stderr is intentionally left as direct `os.Stderr` (no
  pipe-goroutine, no separate log file). If interleaved
  daemon+bridge+test stderr output to the same fd becomes a torn-write
  problem under `t.Parallel()` or heavy logging, a future change can
  add per-bridge log files; out of scope for this hardening pass.
