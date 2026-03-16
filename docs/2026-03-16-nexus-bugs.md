# Nexus Bugs Found During Sharkfin Integration Testing — 2026-03-16

## Bug 1: `vm_patch` requires `root_size` even when not changing it

**Reproduction:**
```
mcp__nexus__vm_patch(id: "passport", env: {"BETTER_AUTH_URL": "http://passport.nexus:3000"})
→ Error: "root_size is required"
```

**Expected:** Patching only `env` should not require `root_size`. Optional fields in a patch operation should remain unchanged when omitted.

**Actual:** The API rejects the request unless `root_size` is provided.

**Workaround attempted:** Passing `root_size: 0`:
```
mcp__nexus__vm_patch(id: "passport", env: {...}, root_size: 0)
→ Error: "bytesize: size must be positive, got '0'"
```

This makes `vm_patch` unusable for updating only environment variables without knowing the current root size.

**Workaround used:** Delete and recreate the VM with the new env vars. This loses all state (DB, logs) and requires reseeding.

**Owning service:** Nexus

**Severity:** Medium — forces destructive recreate for any env var change.

## Bug 2: `vm_create` with `latest` tag fails on non-numeric USER

**Reproduction:**
```
mcp__nexus__vm_create(name: "passport", image: "ghcr.io/work-fort/passport:latest")
→ Error: "non-numeric USER 'node' in image ... parse uid 'node': strconv.ParseUint: parsing 'node': invalid syntax"
```

**Expected:** Nexus should resolve the `node` user from the image's `/etc/passwd` to its numeric UID, or support non-numeric USER directives.

**Actual:** Nexus requires the Dockerfile USER to be a numeric UID. Images using `USER node` (common in Node.js official images) fail.

**Note:** This error appeared with the `latest` tag but not with `v0.0.7` or `v0.0.8` or `v0.1.0`. It's possible the `latest` tag pointed to a different image layer at the time, or the tagged versions use a different base image.

**Owning service:** Nexus

**Severity:** Low — tagged versions work. But `latest` is a common workflow.

## Bug 3: Stale network namespace prevents VM start after unclean shutdown

**Reproduction:** Passport VM was stopped (via host reboot or unclean shutdown). Attempting to start it:
```
mcp__nexus__vm_start(id: "passport")
→ Error: "namespace path: lstat /run/user/1000/nexus/netns/{id}: no such file or directory"
```

Stopping and restarting did not fix it:
```
mcp__nexus__vm_stop(id: "passport") → stopped
mcp__nexus__vm_start(id: "passport") → same error
```

**Expected:** Starting a stopped VM should recreate the network namespace if the old one is gone.

**Actual:** The VM config retains a reference to a stale netns path that no longer exists (cleaned up on host reboot). The VM cannot be started without deleting and recreating it.

**Workaround used:** Delete the VM and create a new one.

**Owning service:** Nexus

**Severity:** Medium — any host reboot or unclean shutdown makes existing VMs unrecoverable without recreation.

## Bug 4: `vm_create` `env` parameter is silently ignored

**Reproduction:**
```
mcp__nexus__vm_create(name: "passport", image: "...", env: {"BETTER_AUTH_URL": "http://passport.nexus:3000"})
→ VM created successfully (no error)

# After starting:
mcp__nexus__vm_exec(id: "passport", cmd: ["env"])
→ No BETTER_AUTH_URL in output — only image defaults (PATH, NODE_VERSION, etc.)
```

**Expected:** Environment variables passed in `env` should be set in the container's environment when it starts.

**Actual:** The `env` parameter is accepted without error but has no effect. The container only has environment variables baked into the image.

**Impact:** Cannot configure runtime behavior (like `BETTER_AUTH_URL`, `DATABASE_URL`, etc.) without rebuilding the image or using `vm_exec` to start processes with inline env vars.

**Owning service:** Nexus

**Severity:** High — environment configuration is fundamental to container operation.
