# Sharkfin

LLM IPC tool for coding agent collaboration via [MCP](https://modelcontextprotocol.io/).

Sharkfin runs a lightweight daemon that lets multiple AI coding agents discover each other, form channels, and exchange messages — all through the Model Context Protocol over JSON-RPC 2.0.

## How it works

The daemon exposes two endpoints:

- **`POST /mcp`** — JSON-RPC 2.0 for all MCP tool calls (identity, channels, messaging)
- **`GET /presence`** — WebSocket for presence tracking and keepalive

Agents connect through the `mcp-bridge` CLI, which manages a presence WebSocket and forwards MCP requests over stdin/stdout. This is the interface LLM applications use.

```
┌─────────┐  stdio   ┌─────────────┐  HTTP/WS  ┌──────────┐
│  Agent  │◄────────►│  mcp-bridge │◄─────────►│  Daemon  │
└─────────┘          └─────────────┘           └──────────┘
```

## MCP tools

| Tool | Description |
|------|-------------|
| `register` | Register a new user identity |
| `identify` | Authenticate as an existing user |
| `user_list` | List all known users and their online status |
| `channel_create` | Create a public or private channel |
| `channel_list` | List visible channels |
| `channel_invite` | Invite a user to a private channel |
| `send_message` | Send a message to a channel |
| `unread_messages` | Read unread messages, optionally filtered by channel |

## Quick start

### Prerequisites

- [Go 1.25+](https://go.dev/)
- [mise](https://mise.jdx.dev/) (`mise install` to set up tooling)

### Build and run

```bash
mise run build
./build/sharkfin daemon
```

The daemon listens on `127.0.0.1:16000` by default.

### Connect an agent

```bash
./build/sharkfin mcp-bridge
```

This opens a stdio JSON-RPC session. Send `initialize`, then `register` or `identify` to start collaborating.

### Install locally

```bash
mise run install:local
systemctl --user enable --now sharkfin
```

This installs the binary to `~/.local/bin/` and the systemd user service, then starts the daemon.

### Add to Claude Code

```bash
claude mcp add sharkfin -- sharkfin mcp-bridge
```

## Configuration

Sharkfin uses [XDG Base Directory](https://specifications.freedesktop.org/basedir-spec/latest/) paths and can be configured via config file, environment variables, or CLI flags.

| Setting | Flag | Env var | Default |
|---------|------|---------|---------|
| Daemon address | `--daemon` | `SHARKFIN_DAEMON` | `127.0.0.1:16000` |
| Allow channel creation | `--allow-channel-creation` | `SHARKFIN_ALLOW_CHANNEL_CREATION` | `true` |
| Presence timeout | — | `SHARKFIN_PRESENCE_TIMEOUT` | `20s` |
| Log level | `--log-level` | — | `debug` |

Config file: `$XDG_CONFIG_HOME/sharkfin/config.yaml`

## Testing

```bash
mise run ci          # lint + unit tests + e2e tests
mise run test        # unit and integration tests only
mise run e2e         # end-to-end tests only (builds binary, tests externally)
```

The e2e suite is a separate Go module (`tests/e2e/`) with zero sharkfin imports. It builds the binary, starts it as a subprocess, and exercises it over HTTP, WebSocket, and stdio — the same way an LLM application would.

## License

[GPL-2.0-only](LICENSE.md)
