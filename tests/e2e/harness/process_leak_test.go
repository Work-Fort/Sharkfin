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
