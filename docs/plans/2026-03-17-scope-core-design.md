# Scope Core — Design Spec

**Goal:** Replace the Go BFF with a shared Rust library crate (`scope-core`) consumed by two binaries: `scope-server` (headless HTTP server for browser access) and `workfort-scope` (Tauri desktop app). Adds persistent notifications, user preferences, and a framework-agnostic service module contract.

**Key Principle:** `scope-core` owns all BFF logic. Consumers are thin transport layers — axum routes for the server, Tauri commands + protocol handler for the desktop app. The Go codebase is deleted entirely.

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│ workfort-scope (Tauri)                          │
│  • Tauri commands + protocol handler            │
│  • Native OS notifications                      │
│  • SQLite always                                │
│  └── imports scope-core                         │
└───────────────────┬─────────────────────────────┘
                    │
                    │  scope-core (library crate)
                    │  • domain types + traits (ports)
                    │  • SQLite + Postgres adapters
                    │  • HTTP/WS proxy
                    │  • Service discovery + health polling
                    │  • Notification subscriber
                    │  • Token management + refresh
                    │  • Config parsing (YAML, XDG)
                    │
┌───────────────────┴─────────────────────────────┐
│ scope-server (axum)                             │
│  • HTTP/WS endpoints                            │
│  • Shell WS (push notifications, state changes) │
│  • SPA serving (embedded mode)                  │
│  • SQLite or Postgres                           │
│  └── imports scope-core                         │
└─────────────────────────────────────────────────┘
```

Both consumers talk to the same backend services:

```
scope-core → Passport (auth, tokens, JWKS)
scope-core → Sharkfin (chat, /notifications/subscribe)
scope-core → Hive, Combine, etc. (future services)
```

---

## Crate Layout

```
scope/lead/
├── Cargo.toml                # Workspace manifest
├── crates/
│   ├── scope-core/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       ├── lib.rs
│   │       ├── domain/
│   │       │   ├── mod.rs
│   │       │   ├── fort.rs          # Fort, ServiceConfig
│   │       │   ├── notification.rs  # Notification, Urgency, NotificationLevel
│   │       │   ├── session.rs       # FortTokens, UserInfo, AuthState
│   │       │   └── ports.rs         # Store trait
│   │       ├── infra/
│   │       │   ├── mod.rs
│   │       │   ├── sqlite/          # SQLite Store adapter
│   │       │   ├── postgres/        # Postgres Store adapter
│   │       │   ├── proxy/           # HTTP + WS reverse proxy
│   │       │   └── discovery/       # Service health polling + notification subscriber
│   │       └── config/
│   │           └── mod.rs           # YAML config, XDG paths
│   │
│   └── scope-server/
│       ├── Cargo.toml
│       └── src/
│           └── main.rs              # axum routes wrapping scope-core
│
├── src-tauri/                       # Tauri app (refactored to use scope-core)
│   ├── Cargo.toml
│   └── src/
│       ├── main.rs
│       └── lib.rs
│
├── web/                             # Shell SPA (unchanged)
│   ├── shell/
│   └── packages/                    # ui, ui-solid, ui-react, ui-svelte, ui-vue
│
└── docs/
```

**Deleted:** All Go code — `cmd/`, `internal/`, `pkg/`, `go.mod`, `go.sum`.

---

## Domain Layer

### Ports (Store Trait)

```rust
#[async_trait]
pub trait Store: Send + Sync {
    // Fort config
    async fn list_forts(&self) -> Result<Vec<Fort>>;
    async fn get_fort(&self, name: &str) -> Result<Fort>;
    async fn upsert_fort(&self, fort: &Fort) -> Result<()>;
    async fn delete_fort(&self, name: &str) -> Result<()>;
    async fn get_active_fort(&self) -> Result<Option<String>>;
    async fn set_active_fort(&self, name: &str) -> Result<()>;

    // Notifications
    async fn insert_notification(&self, n: &Notification) -> Result<i64>;
    async fn list_notifications(&self, limit: i64, before_id: Option<i64>) -> Result<Vec<Notification>>;
    async fn unread_count(&self) -> Result<i64>;
    async fn mark_read(&self, up_to_id: i64) -> Result<()>;

    // User preferences
    async fn get_preference(&self, service: &str) -> Result<NotificationLevel>;
    async fn set_preference(&self, service: &str, level: NotificationLevel) -> Result<()>;
}
```

### Types

```rust
// Fort configuration
pub struct Fort {
    pub name: String,
    pub local: bool,
    pub gateway: Option<String>,
    pub services: Vec<ServiceConfig>,
}

pub struct ServiceConfig {
    pub url: String,
}

// Discovered at runtime
pub struct TrackedService {
    pub name: String,
    pub label: String,
    pub route: String,
    pub ui: bool,
    pub connected: bool,
    pub setup_mode: bool,
    pub admin_only: bool,
    pub ws_paths: Vec<String>,
    pub notification_path: Option<String>,
}

// Notifications
pub enum Urgency {
    Passive,   // Badge only — CI passed, background update
    Active,    // Sound + toast — mention, DM, needs attention
}

pub enum NotificationLevel {
    Mute,          // Don't surface at all
    PassiveOnly,   // Badge only, even for active urgency
    AllowUrgent,   // Respect service's urgency level (default)
}

pub struct Notification {
    pub id: Option<i64>,
    pub service: String,
    pub title: String,
    pub body: Option<String>,
    pub urgency: Urgency,
    pub route: Option<String>,
    pub read: bool,
    pub created_at: DateTime<Utc>,
}

// Session / auth
pub struct FortTokens {
    pub jwt: String,
    pub refresh_token: String,
    pub expiry: Instant,
    pub auth_url: String,
}

pub struct UserInfo {
    pub id: String,
    pub email: String,
    pub name: String,
    pub role: Option<String>,
}
```

---

## Infrastructure Layer

### Store Adapters (SQLite + Postgres)

Both implement `Store` trait with raw SQL via `sqlx`. Postgres adapter uses native types (`TIMESTAMPTZ`, `JSONB`) where beneficial. Migrations embedded at compile time via `sqlx::migrate!()`.

### HTTP/WS Proxy

Reverse proxy using `reqwest` (HTTP) and `tokio-tungstenite` (WebSocket). Attaches per-fort JWT to outgoing requests. 401 retry: if upstream returns 401, refreshes token and retries once.

### Service Discovery

Polls `/ui/health` on configured service URLs. Parses health manifests. Tracks connection state. Detects `admin_only`, `setup_mode`, `notification_path` fields.

### Notification Subscriber

Connects to each service's `notification_path` WebSocket (if declared in health manifest). Receives pre-classified notifications:

```json
{
  "title": "New message in #general",
  "body": "@admin mentioned you",
  "urgency": "active",
  "route": "/chat"
}
```

Applies user preferences (mute / passive-only / allow-urgent). Stores in DB. Calls platform-specific delivery callback.

---

## Database Schema

### SQLite

```sql
CREATE TABLE forts (
    name TEXT PRIMARY KEY,
    local INTEGER NOT NULL DEFAULT 1,
    gateway TEXT,
    active INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE fort_services (
    fort_name TEXT NOT NULL REFERENCES forts(name) ON DELETE CASCADE,
    url TEXT NOT NULL,
    PRIMARY KEY (fort_name, url)
);

CREATE TABLE notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    urgency TEXT NOT NULL CHECK (urgency IN ('passive', 'active')),
    route TEXT,
    read INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_notifications_unread ON notifications (read, created_at DESC);

CREATE TABLE preferences (
    service TEXT PRIMARY KEY,
    level TEXT NOT NULL DEFAULT 'allow_urgent'
        CHECK (level IN ('mute', 'passive_only', 'allow_urgent'))
);
```

### Postgres

Same structure with native types:

- `BOOLEAN` instead of `INTEGER` for flags
- `BIGSERIAL` instead of `INTEGER PRIMARY KEY AUTOINCREMENT`
- `TIMESTAMPTZ` instead of `TEXT` for timestamps
- `JSONB metadata` column on notifications (extensible per-service data)

---

## scope-server (axum)

Headless HTTP server. Supports SQLite (default) and Postgres (`--database-url`).

### Endpoints

| Route | Method | Purpose |
|---|---|---|
| `/ws/shell` | WS | Push notifications, service state, version updates |
| `/api/forts` | GET | List configured forts |
| `/api/session` | GET | Auth state + user + role |
| `/api/services` | GET | Service list (admin-only filtered by role) |
| `/api/notifications` | GET | Notification inbox (paginated) |
| `/api/notifications/read` | POST | Mark notifications as read |
| `/api/preferences/:service` | GET/PUT | Per-service notification level |
| `/forts/{fort}/api/*` | ANY | HTTP proxy to services |
| `/forts/{fort}/ws/*` | WS | WebSocket proxy to services |
| `/*` | GET | SPA fallback (embedded mode) |

### Shell WS Protocol

Single WebSocket from browser to scope-server. Server pushes events:

```typescript
type ShellEvent =
  | { type: "notification", data: Notification }
  | { type: "services_changed", data: TrackedService[] }
  | { type: "version_available", data: { version: string } }
  | { type: "connection_state", data: { service: string, connected: boolean } }
```

Replaces polling. Service state changes, notifications, and version updates arrive in real-time.

### Static Asset Serving

Two modes:

- **Embedded mode** (local dev, single binary) — serves shell SPA from disk or embedded assets. `remoteEntry.js` and service assets proxied through to services.
- **External mode** (server deployment) — SPA served by CDN/nginx. scope-server is API + WS only. Pushes `version_available` event when UI version changes, prompting browser refresh without killing WS connections.

---

## workfort-scope (Tauri)

Desktop app. Always SQLite. Same `scope-core` logic, different transport.

### Tauri-Specific Behavior

- **Protocol handler** — intercepts `https://` requests from webview, routes `/api/*` and `/forts/*` through scope-core proxy
- **Native notifications** — `tauri-plugin-notification` for OS-level alerts (macOS Notification Center, Windows toast, Linux xdg-notification, Android notifications)
- **Tauri commands** — `login`, `logout`, `get_user`, `get_forts`, `add_fort`, `remove_fort`, `set_active_fort`, `get_notifications`, `mark_read`, `get_preference`, `set_preference`
- **Event emission** — `app_handle.emit("notification", &notif)` pushes to webview (in-app toast/badge) alongside native OS notification

### Notification Delivery (Tauri)

When a notification arrives from a service:

1. Store in SQLite (via scope-core)
2. Push to webview via Tauri event (in-app toast/badge)
3. Fire native OS notification if urgency is `active` and user preference allows it

---

## Service Module Contract (Shell SPA)

Framework-agnostic Module Federation remote interface:

```typescript
interface ServiceManifest {
  name: string;
  label: string;
  route: string;
  display: "nav" | "menu";  // nav = tab, menu = hamburger link
}

interface ServiceModule {
  mount(el: HTMLElement, props: { connected: boolean }): void;
  unmount(el: HTMLElement): void;
  manifest: ServiceManifest;
  mountSidebar?(el: HTMLElement): void;
  unmountSidebar?(el: HTMLElement): void;
}
```

- `mount`/`unmount` replace the SolidJS-specific `<Dynamic>` pattern
- Each framework implements mount differently (React: `createRoot`, Solid: `render`, Svelte: `new App`, Vue: `createApp`)
- `display: "nav"` shows as tab in nav bar (Chat, Hive)
- `display: "menu"` shows as link in hamburger menu (Admin)
- Both mount into the same content area when selected
- Notifications are server-side — services push to `/notifications/subscribe`, scope-core handles delivery. MF remotes are purely UI.

### Service Health Manifest (updated)

Services declare notification support in `/ui/health`:

```json
{
  "name": "sharkfin",
  "label": "Chat",
  "route": "/chat",
  "ws_paths": ["/ws", "/presence"],
  "notification_path": "/notifications/subscribe",
  "admin_only": false
}
```

Services without `notification_path` don't participate in notifications.

---

## Notification Preference Matrix

| Service sends | User pref: Mute | User pref: Passive only | User pref: Allow urgent |
|---|---|---|---|
| `passive` | Hidden | Badge only | Badge only |
| `active` | Hidden | Badge only | Sound + toast + badge |

Default preference for all services: `allow_urgent`.

---

## Configuration

YAML config at XDG path (`$XDG_CONFIG_HOME/workfort/config.yaml`, default `~/.config/workfort/config.yaml`).

```yaml
# scope-server config
listen: "127.0.0.1:16100"
database: "~/.local/state/workfort/scope.db"  # or postgres://...

# Fort definitions (same format as current Go BFF)
forts:
  local:
    local: true
    services:
      - url: "http://passport.nexus:3000"
      - url: "http://127.0.0.1:16000"
```

Tauri app stores fort config in its database instead of YAML (managed via UI). The YAML config is for scope-server / headless deployments.

---

## Dependencies

### scope-core

- `sqlx` — async SQL (SQLite + Postgres, compile-time checked queries)
- `reqwest` — HTTP client for proxy and discovery
- `tokio-tungstenite` — WebSocket client for proxy and notification subscriber
- `serde` + `serde_yaml` + `serde_json` — serialization
- `tokio` — async runtime
- `chrono` — timestamps
- `url` — URL parsing
- `directories` — XDG Base Directory

### scope-server (additional)

- `axum` — HTTP framework
- `tower` — middleware
- `tower-http` — CORS, static serving, compression

### workfort-scope (additional)

- `tauri` v2
- `tauri-plugin-notification` — native OS notifications

---

## What This Unblocks

1. **Passport admin UI** — admin-only service filtering, menu display type
2. **Cross-app notifications** — Sharkfin mentions while in Hive, CI alerts from Combine
3. **Native desktop notifications** — OS-level alerts via Tauri
4. **Framework-agnostic remotes** — React (Passport), Solid (Sharkfin), future Svelte/Vue services
5. **Server deployments** — scope-server with Postgres, UI served externally, zero-downtime UI updates
6. **Gateway path** — notification subscriber connects to gateway instead of individual services

## Supersedes

- Go BFF (`cmd/web/`, `internal/infra/httpapi/`)
- Go TUI (`cmd/chat/`, `internal/chat/`, `pkg/`)
- All Go code in the scope repo
