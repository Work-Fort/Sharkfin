# Bug: HTTP Server Missing Timeouts

## Problem

`pkg/daemon/server.go` lines 82-85 — the `http.Server` is created with only
`Addr` and `Handler`. No `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, or
`ReadHeaderTimeout` are set.

This exposes the service to Slowloris-class attacks where a client holds
connections open indefinitely by sending data slowly.

## Fix

Add timeouts to the `http.Server` struct:

```go
ReadTimeout:       15 * time.Second
WriteTimeout:      15 * time.Second
IdleTimeout:       60 * time.Second
ReadHeaderTimeout: 5 * time.Second
```

See `codex/src/architecture/go-service-patterns.md` for the standard pattern.

## Severity

High — affects any internet-exposed deployment.
