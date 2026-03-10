# Server Version Query Design

## Problem

Clients currently have no way to query the server version. This makes
debugging difficult — when something breaks, neither the operator nor the
agent can tell which version of the daemon is running.

The build system already injects a version string via ldflags
(`cmd.Version`), and the CLI prints it with `sharkfin --version`. But no
protocol exposes it to connected clients.

## Requirements

1. **WS clients** can see the server version in two ways:
   - The `hello` envelope sent on connect includes a `version` field.
   - A new `version` request type returns the server version on demand.
2. **MCP clients** see the server version in the standard MCP `initialize`
   response's `serverInfo` field (replacing the hardcoded `"0.1.0"`).
3. No new MCP tool is needed — `serverInfo` is the standard mechanism.

## Design

### Version plumbing

`cmd.Version` (set by ldflags at build time) is passed from
`cmd/daemon/daemon.go` → `NewServer` → `NewSharkfinMCP` + `NewWSHandler`.
Each constructor gains a `version string` parameter.

### MCP: serverInfo

`mcp-go`'s `server.NewMCPServer(name, version, ...)` already populates the
`serverInfo` field in the MCP `initialize` response. Today we pass the
hardcoded string `"0.1.0"`. We replace it with the actual build version.

When `cmd.Version` is empty (e.g. `go run` without ldflags), we default to
`"dev"`.

### WS: hello envelope

The existing hello message:

```json
{"type":"hello","d":{"heartbeat_interval":15}}
```

Becomes:

```json
{"type":"hello","d":{"heartbeat_interval":15,"version":"0.2.0"}}
```

### WS: version request

A new request type `version` is available both before and after
identification (like `ping`). No permission check needed.

Request:
```json
{"type":"version","ref":"v1"}
```

Response:
```json
{"type":"reply","ref":"v1","ok":true,"d":{"version":"0.2.0"}}
```

### Default value

When `cmd.Version` is empty (dev builds without ldflags), use `"dev"` as
the version string everywhere.

## Non-goals

- Version negotiation or compatibility checks.
- Exposing the MCP protocol version separately — `serverInfo` already
  carries this via the standard MCP handshake.
- Adding a `--version` flag to the daemon subcommand (the root command
  already has it).
