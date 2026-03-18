# Scope Core Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the Go BFF with a Rust workspace containing `scope-core` (library), `scope-server` (axum binary), and refactored `workfort-scope` (Tauri app).

**Architecture:** Hexagonal — domain traits (ports) in `scope-core`, SQLite and Postgres store adapters (infra), consumed by two thin transport layers: axum HTTP server and Tauri desktop app. Persistent notification system with server-side service subscription.

**Tech Stack:** Rust 1.94, sqlx (SQLite + Postgres), axum, tokio, reqwest, tokio-tungstenite, Tauri v2, serde + serde_yaml

**Design Doc:** `docs/plans/2026-03-17-scope-core-design.md`

**Working directory:** `~/Work/WorkFort/scope/lead`

---

## Phase 1: Workspace Setup + Domain Layer

### Task 1: Create Cargo Workspace and Remove Go

**Files:**
- Create: `Cargo.toml` (workspace root)
- Create: `crates/scope-core/Cargo.toml`
- Create: `crates/scope-core/src/lib.rs`
- Create: `crates/scope-server/Cargo.toml`
- Create: `crates/scope-server/src/main.rs`
- Modify: `src-tauri/Cargo.toml` — add `scope-core` dependency
- Delete: `cmd/`, `internal/`, `pkg/`, `go.mod`, `go.sum`
- Modify: `mise.toml` — remove `go` from tools

**Step 1: Create workspace Cargo.toml at repo root**

```toml
[workspace]
resolver = "2"
members = [
    "crates/scope-core",
    "crates/scope-server",
    "src-tauri",
]
```

**Step 2: Create scope-core crate**

`crates/scope-core/Cargo.toml`:
```toml
[package]
name = "scope-core"
version = "0.1.0"
edition = "2021"

[dependencies]
async-trait = "0.1"
chrono = { version = "0.4", features = ["serde"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_yaml = "0.9"
sqlx = { version = "0.8", features = ["runtime-tokio", "sqlite", "postgres", "chrono", "migrate"] }
tokio = { version = "1", features = ["full"] }
reqwest = { version = "0.12", features = ["json", "rustls-tls"], default-features = false }
tokio-tungstenite = { version = "0.26", features = ["rustls-tls-webpki-roots"] }
url = "2"
directories = "6"
thiserror = "2"
log = "0.4"

[dev-dependencies]
tokio = { version = "1", features = ["test-util", "macros"] }
```

`crates/scope-core/src/lib.rs`:
```rust
pub mod config;
pub mod domain;
pub mod infra;
```

**Step 3: Create scope-server crate**

`crates/scope-server/Cargo.toml`:
```toml
[package]
name = "scope-server"
version = "0.1.0"
edition = "2021"

[dependencies]
scope-core = { path = "../scope-core" }
axum = { version = "0.8", features = ["ws"] }
tokio = { version = "1", features = ["full"] }
tower = "0.5"
tower-http = { version = "0.6", features = ["cors", "fs", "compression-gzip"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
log = "0.4"
env_logger = "0.11"
```

`crates/scope-server/src/main.rs`:
```rust
#[tokio::main]
async fn main() {
    env_logger::init();
    log::info!("scope-server starting");
    // TODO: wire up in subsequent tasks
}
```

**Step 4: Update src-tauri/Cargo.toml to use workspace**

Add to `[dependencies]`:
```toml
scope-core = { path = "../crates/scope-core" }
```

**Step 5: Verify workspace compiles**

```bash
cd ~/Work/WorkFort/scope/lead && cargo check
```

Expected: compiles with no errors (just warnings about unused code).

**Step 6: Remove Go code**

```bash
cd ~/Work/WorkFort/scope/lead
rm -rf cmd/ internal/ pkg/ go.mod go.sum
```

**Step 7: Update mise.toml — remove go**

Remove `go = "latest"` from `[tools]`.

**Step 8: Verify everything still compiles**

```bash
cd ~/Work/WorkFort/scope/lead && cargo check
```

**Step 9: Commit**

```bash
git add -A && git commit -m "feat: create Rust workspace, remove Go BFF

scope-core library crate + scope-server binary crate. Tauri app
uses workspace. All Go code removed (BFF, TUI, packages)."
```

---

### Task 2: Domain Types

**Files:**
- Create: `crates/scope-core/src/domain/mod.rs`
- Create: `crates/scope-core/src/domain/fort.rs`
- Create: `crates/scope-core/src/domain/notification.rs`
- Create: `crates/scope-core/src/domain/session.rs`
- Create: `crates/scope-core/src/domain/ports.rs`

**Step 1: Create domain module**

`crates/scope-core/src/domain/mod.rs`:
```rust
pub mod fort;
pub mod notification;
pub mod ports;
pub mod session;

pub use fort::*;
pub use notification::*;
pub use ports::*;
pub use session::*;
```

**Step 2: Fort types**

`crates/scope-core/src/domain/fort.rs`:
```rust
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Fort {
    pub name: String,
    pub local: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub gateway: Option<String>,
    pub services: Vec<ServiceConfig>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServiceConfig {
    pub url: String,
}

/// Discovered at runtime by probing /ui/health.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TrackedService {
    pub name: String,
    pub label: String,
    pub route: String,
    pub ui: bool,
    pub connected: bool,
    #[serde(default)]
    pub setup_mode: bool,
    #[serde(default)]
    pub admin_only: bool,
    #[serde(default)]
    pub ws_paths: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub notification_path: Option<String>,
}
```

**Step 3: Notification types**

`crates/scope-core/src/domain/notification.rs`:
```rust
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Urgency {
    Passive,
    Active,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum NotificationLevel {
    Mute,
    PassiveOnly,
    AllowUrgent,
}

impl Default for NotificationLevel {
    fn default() -> Self {
        Self::AllowUrgent
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Notification {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<i64>,
    pub service: String,
    pub title: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub body: Option<String>,
    pub urgency: Urgency,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub route: Option<String>,
    #[serde(default)]
    pub read: bool,
    pub created_at: DateTime<Utc>,
}
```

**Step 4: Session types**

`crates/scope-core/src/domain/session.rs`:
```rust
use serde::{Deserialize, Serialize};
use std::time::Instant;

pub struct FortTokens {
    pub jwt: String,
    pub refresh_token: String,
    pub expiry: Instant,
    pub auth_url: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UserInfo {
    pub id: String,
    pub email: String,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub role: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthState {
    pub authenticated: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user: Option<UserInfo>,
}
```

**Step 5: Store trait (ports)**

`crates/scope-core/src/domain/ports.rs`:
```rust
use async_trait::async_trait;

use super::{Fort, Notification, NotificationLevel};

pub type Result<T> = std::result::Result<T, crate::Error>;

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

**Step 6: Add Error type to lib.rs**

Update `crates/scope-core/src/lib.rs`:
```rust
pub mod config;
pub mod domain;
pub mod infra;

#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("not found: {0}")]
    NotFound(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("config error: {0}")]
    Config(String),
    #[error("{0}")]
    Other(String),
}
```

**Step 7: Create placeholder infra and config modules**

`crates/scope-core/src/infra/mod.rs`:
```rust
pub mod sqlite;
// pub mod postgres;  // Task 4
// pub mod proxy;     // Task 5
// pub mod discovery;  // Task 6
```

`crates/scope-core/src/config/mod.rs`:
```rust
// TODO: YAML config parsing — Task 7
```

`crates/scope-core/src/infra/sqlite/mod.rs`:
```rust
// TODO: SQLite store adapter — Task 3
```

**Step 8: Verify compiles**

```bash
cd ~/Work/WorkFort/scope/lead && cargo check
```

**Step 9: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add crates/scope-core/src/
git commit -m "feat(scope-core): add domain types and Store trait"
```

---

## Phase 2: Store Adapters

### Task 3: SQLite Store Adapter

**Files:**
- Create: `crates/scope-core/src/infra/sqlite/mod.rs`
- Create: `crates/scope-core/migrations/sqlite/001_initial.sql`
- Create: `crates/scope-core/src/infra/sqlite/tests.rs`

**Step 1: Create SQLite migration**

`crates/scope-core/migrations/sqlite/001_initial.sql`:
```sql
CREATE TABLE IF NOT EXISTS forts (
    name TEXT PRIMARY KEY,
    local INTEGER NOT NULL DEFAULT 1,
    gateway TEXT,
    active INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS fort_services (
    fort_name TEXT NOT NULL REFERENCES forts(name) ON DELETE CASCADE,
    url TEXT NOT NULL,
    PRIMARY KEY (fort_name, url)
);

CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    urgency TEXT NOT NULL CHECK (urgency IN ('passive', 'active')),
    route TEXT,
    read INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_notifications_unread
    ON notifications (read, created_at DESC);

CREATE TABLE IF NOT EXISTS preferences (
    service TEXT PRIMARY KEY,
    level TEXT NOT NULL DEFAULT 'allow_urgent'
        CHECK (level IN ('mute', 'passive_only', 'allow_urgent'))
);
```

**Step 2: Write failing tests**

`crates/scope-core/src/infra/sqlite/tests.rs`:
```rust
#[cfg(test)]
mod tests {
    use super::SqliteStore;
    use crate::domain::*;
    use chrono::Utc;

    async fn test_store() -> SqliteStore {
        SqliteStore::open(":memory:").await.unwrap()
    }

    #[tokio::test]
    async fn fort_crud() {
        let store = test_store().await;

        // Empty initially
        let forts = store.list_forts().await.unwrap();
        assert!(forts.is_empty());

        // Insert
        let fort = Fort {
            name: "test".into(),
            local: true,
            gateway: None,
            services: vec![ServiceConfig { url: "http://localhost:16000".into() }],
        };
        store.upsert_fort(&fort).await.unwrap();

        // Read back
        let forts = store.list_forts().await.unwrap();
        assert_eq!(forts.len(), 1);
        assert_eq!(forts[0].name, "test");
        assert_eq!(forts[0].services.len(), 1);

        // Delete
        store.delete_fort("test").await.unwrap();
        let forts = store.list_forts().await.unwrap();
        assert!(forts.is_empty());
    }

    #[tokio::test]
    async fn active_fort() {
        let store = test_store().await;

        let fort = Fort { name: "a".into(), local: true, gateway: None, services: vec![] };
        store.upsert_fort(&fort).await.unwrap();

        assert!(store.get_active_fort().await.unwrap().is_none());

        store.set_active_fort("a").await.unwrap();
        assert_eq!(store.get_active_fort().await.unwrap().unwrap(), "a");
    }

    #[tokio::test]
    async fn notification_crud() {
        let store = test_store().await;

        let n = Notification {
            id: None,
            service: "sharkfin".into(),
            title: "New message".into(),
            body: Some("@admin mentioned you".into()),
            urgency: Urgency::Active,
            route: Some("/chat".into()),
            read: false,
            created_at: Utc::now(),
        };
        let id = store.insert_notification(&n).await.unwrap();
        assert!(id > 0);

        // Unread count
        assert_eq!(store.unread_count().await.unwrap(), 1);

        // List
        let list = store.list_notifications(10, None).await.unwrap();
        assert_eq!(list.len(), 1);
        assert_eq!(list[0].title, "New message");

        // Mark read
        store.mark_read(id).await.unwrap();
        assert_eq!(store.unread_count().await.unwrap(), 0);
    }

    #[tokio::test]
    async fn preferences() {
        let store = test_store().await;

        // Default
        let level = store.get_preference("sharkfin").await.unwrap();
        assert_eq!(level, NotificationLevel::AllowUrgent);

        // Set
        store.set_preference("sharkfin", NotificationLevel::Mute).await.unwrap();
        let level = store.get_preference("sharkfin").await.unwrap();
        assert_eq!(level, NotificationLevel::Mute);
    }
}
```

**Step 3: Run tests to verify they fail**

```bash
cd ~/Work/WorkFort/scope/lead && cargo test -p scope-core
```

Expected: FAIL (SqliteStore doesn't exist yet)

**Step 4: Implement SqliteStore**

`crates/scope-core/src/infra/sqlite/mod.rs`:
```rust
use sqlx::sqlite::{SqliteConnectOptions, SqlitePoolOptions};
use sqlx::SqlitePool;
use async_trait::async_trait;
use chrono::{DateTime, Utc};

use crate::domain::*;
use crate::Error;

mod tests;

pub struct SqliteStore {
    pool: SqlitePool,
}

impl SqliteStore {
    pub async fn open(path: &str) -> std::result::Result<Self, Error> {
        let opts = if path == ":memory:" {
            SqliteConnectOptions::new().filename(":memory:").create_if_missing(true)
        } else {
            SqliteConnectOptions::new()
                .filename(path)
                .create_if_missing(true)
                .journal_mode(sqlx::sqlite::SqliteJournalMode::Wal)
                .busy_timeout(std::time::Duration::from_secs(5))
                .synchronous(sqlx::sqlite::SqliteSynchronous::Normal)
        };

        let pool = SqlitePoolOptions::new()
            .max_connections(1)
            .connect_with(opts)
            .await?;

        // Run migrations
        sqlx::query(include_str!("../../../migrations/sqlite/001_initial.sql"))
            .execute(&pool)
            .await?;

        Ok(Self { pool })
    }
}

#[async_trait]
impl Store for SqliteStore {
    async fn list_forts(&self) -> Result<Vec<Fort>> {
        let rows = sqlx::query_as::<_, (String, bool, Option<String>)>(
            "SELECT name, local, gateway FROM forts ORDER BY name"
        )
        .fetch_all(&self.pool)
        .await?;

        let mut forts = Vec::new();
        for (name, local, gateway) in rows {
            let svc_rows = sqlx::query_as::<_, (String,)>(
                "SELECT url FROM fort_services WHERE fort_name = ?"
            )
            .bind(&name)
            .fetch_all(&self.pool)
            .await?;

            forts.push(Fort {
                name,
                local,
                gateway,
                services: svc_rows.into_iter().map(|(url,)| ServiceConfig { url }).collect(),
            });
        }
        Ok(forts)
    }

    async fn get_fort(&self, name: &str) -> Result<Fort> {
        let row = sqlx::query_as::<_, (String, bool, Option<String>)>(
            "SELECT name, local, gateway FROM forts WHERE name = ?"
        )
        .bind(name)
        .fetch_optional(&self.pool)
        .await?
        .ok_or_else(|| Error::NotFound(format!("fort: {name}")))?;

        let svc_rows = sqlx::query_as::<_, (String,)>(
            "SELECT url FROM fort_services WHERE fort_name = ?"
        )
        .bind(name)
        .fetch_all(&self.pool)
        .await?;

        Ok(Fort {
            name: row.0,
            local: row.1,
            gateway: row.2,
            services: svc_rows.into_iter().map(|(url,)| ServiceConfig { url }).collect(),
        })
    }

    async fn upsert_fort(&self, fort: &Fort) -> Result<()> {
        sqlx::query(
            "INSERT INTO forts (name, local, gateway) VALUES (?, ?, ?)
             ON CONFLICT(name) DO UPDATE SET local = excluded.local, gateway = excluded.gateway"
        )
        .bind(&fort.name)
        .bind(fort.local)
        .bind(&fort.gateway)
        .execute(&self.pool)
        .await?;

        // Replace services
        sqlx::query("DELETE FROM fort_services WHERE fort_name = ?")
            .bind(&fort.name)
            .execute(&self.pool)
            .await?;

        for svc in &fort.services {
            sqlx::query("INSERT INTO fort_services (fort_name, url) VALUES (?, ?)")
                .bind(&fort.name)
                .bind(&svc.url)
                .execute(&self.pool)
                .await?;
        }
        Ok(())
    }

    async fn delete_fort(&self, name: &str) -> Result<()> {
        sqlx::query("DELETE FROM forts WHERE name = ?")
            .bind(name)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn get_active_fort(&self) -> Result<Option<String>> {
        let row = sqlx::query_as::<_, (String,)>(
            "SELECT name FROM forts WHERE active = 1 LIMIT 1"
        )
        .fetch_optional(&self.pool)
        .await?;
        Ok(row.map(|(name,)| name))
    }

    async fn set_active_fort(&self, name: &str) -> Result<()> {
        sqlx::query("UPDATE forts SET active = 0").execute(&self.pool).await?;
        sqlx::query("UPDATE forts SET active = 1 WHERE name = ?")
            .bind(name)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn insert_notification(&self, n: &Notification) -> Result<i64> {
        let row = sqlx::query_as::<_, (i64,)>(
            "INSERT INTO notifications (service, title, body, urgency, route, created_at)
             VALUES (?, ?, ?, ?, ?, ?)
             RETURNING id"
        )
        .bind(&n.service)
        .bind(&n.title)
        .bind(&n.body)
        .bind(serde_json::to_string(&n.urgency).unwrap().trim_matches('"'))
        .bind(&n.route)
        .bind(n.created_at.to_rfc3339())
        .fetch_one(&self.pool)
        .await?;
        Ok(row.0)
    }

    async fn list_notifications(&self, limit: i64, before_id: Option<i64>) -> Result<Vec<Notification>> {
        let rows = if let Some(bid) = before_id {
            sqlx::query_as::<_, (i64, String, String, Option<String>, String, Option<String>, bool, String)>(
                "SELECT id, service, title, body, urgency, route, read, created_at
                 FROM notifications WHERE id < ? ORDER BY created_at DESC LIMIT ?"
            )
            .bind(bid)
            .bind(limit)
            .fetch_all(&self.pool)
            .await?
        } else {
            sqlx::query_as::<_, (i64, String, String, Option<String>, String, Option<String>, bool, String)>(
                "SELECT id, service, title, body, urgency, route, read, created_at
                 FROM notifications ORDER BY created_at DESC LIMIT ?"
            )
            .bind(limit)
            .fetch_all(&self.pool)
            .await?
        };

        rows.into_iter().map(|(id, service, title, body, urgency, route, read, created_at)| {
            Ok(Notification {
                id: Some(id),
                service,
                title,
                body,
                urgency: match urgency.as_str() {
                    "active" => Urgency::Active,
                    _ => Urgency::Passive,
                },
                route,
                read,
                created_at: DateTime::parse_from_rfc3339(&created_at)
                    .map(|dt| dt.with_timezone(&Utc))
                    .unwrap_or_else(|_| Utc::now()),
            })
        }).collect()
    }

    async fn unread_count(&self) -> Result<i64> {
        let row = sqlx::query_as::<_, (i64,)>(
            "SELECT COUNT(*) FROM notifications WHERE read = 0"
        )
        .fetch_one(&self.pool)
        .await?;
        Ok(row.0)
    }

    async fn mark_read(&self, up_to_id: i64) -> Result<()> {
        sqlx::query("UPDATE notifications SET read = 1 WHERE id <= ? AND read = 0")
            .bind(up_to_id)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn get_preference(&self, service: &str) -> Result<NotificationLevel> {
        let row = sqlx::query_as::<_, (String,)>(
            "SELECT level FROM preferences WHERE service = ?"
        )
        .bind(service)
        .fetch_optional(&self.pool)
        .await?;

        Ok(match row {
            Some((level,)) => match level.as_str() {
                "mute" => NotificationLevel::Mute,
                "passive_only" => NotificationLevel::PassiveOnly,
                _ => NotificationLevel::AllowUrgent,
            },
            None => NotificationLevel::default(),
        })
    }

    async fn set_preference(&self, service: &str, level: NotificationLevel) -> Result<()> {
        let level_str = match level {
            NotificationLevel::Mute => "mute",
            NotificationLevel::PassiveOnly => "passive_only",
            NotificationLevel::AllowUrgent => "allow_urgent",
        };
        sqlx::query(
            "INSERT INTO preferences (service, level) VALUES (?, ?)
             ON CONFLICT(service) DO UPDATE SET level = excluded.level"
        )
        .bind(service)
        .bind(level_str)
        .execute(&self.pool)
        .await?;
        Ok(())
    }
}
```

**Step 5: Run tests**

```bash
cd ~/Work/WorkFort/scope/lead && cargo test -p scope-core
```

Expected: ALL PASS

**Step 6: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add crates/scope-core/
git commit -m "feat(scope-core): SQLite store adapter with full CRUD"
```

---

### Task 4: Postgres Store Adapter

**Files:**
- Create: `crates/scope-core/src/infra/postgres/mod.rs`
- Create: `crates/scope-core/migrations/postgres/001_initial.sql`
- Modify: `crates/scope-core/src/infra/mod.rs` — add postgres module

Same trait implementation as SQLite but using Postgres-native types (`BOOLEAN`, `BIGSERIAL`, `TIMESTAMPTZ`, `JSONB`). The implementation follows the same pattern — implement all `Store` trait methods with Postgres SQL syntax.

**Step 1: Create Postgres migration**

`crates/scope-core/migrations/postgres/001_initial.sql`:
```sql
CREATE TABLE IF NOT EXISTS forts (
    name TEXT PRIMARY KEY,
    local BOOLEAN NOT NULL DEFAULT true,
    gateway TEXT,
    active BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS fort_services (
    fort_name TEXT NOT NULL REFERENCES forts(name) ON DELETE CASCADE,
    url TEXT NOT NULL,
    PRIMARY KEY (fort_name, url)
);

CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    service TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    urgency TEXT NOT NULL CHECK (urgency IN ('passive', 'active')),
    route TEXT,
    read BOOLEAN NOT NULL DEFAULT false,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notifications_unread
    ON notifications (read, created_at DESC);

CREATE TABLE IF NOT EXISTS preferences (
    service TEXT PRIMARY KEY,
    level TEXT NOT NULL DEFAULT 'allow_urgent'
        CHECK (level IN ('mute', 'passive_only', 'allow_urgent'))
);
```

**Step 2: Implement PostgresStore**

`crates/scope-core/src/infra/postgres/mod.rs`:

Same pattern as SqliteStore but with `PgPool`, Postgres bind syntax (`$1`, `$2` instead of `?`), and native boolean/timestamp handling. The struct and trait impl mirror the SQLite version with Postgres SQL syntax.

**Step 3: Update infra/mod.rs**

```rust
pub mod sqlite;
pub mod postgres;
```

**Step 4: Add store_from_url factory function to lib.rs**

```rust
use std::sync::Arc;

pub async fn open_store(url: &str) -> Result<Arc<dyn domain::Store>, Error> {
    if url.starts_with("postgres://") || url.starts_with("postgresql://") {
        let store = infra::postgres::PostgresStore::open(url).await?;
        Ok(Arc::new(store))
    } else {
        let store = infra::sqlite::SqliteStore::open(url).await?;
        Ok(Arc::new(store))
    }
}
```

**Step 5: Verify compiles**

```bash
cd ~/Work/WorkFort/scope/lead && cargo check
```

Note: Postgres tests require a running Postgres instance — skip in CI unless available. SQLite tests cover the trait contract.

**Step 6: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add crates/scope-core/
git commit -m "feat(scope-core): Postgres store adapter with JSONB and TIMESTAMPTZ"
```

---

## Phase 3: Infrastructure — Proxy, Discovery, Config

### Task 5: HTTP/WS Proxy

**Files:**
- Create: `crates/scope-core/src/infra/proxy/mod.rs`
- Modify: `crates/scope-core/src/infra/mod.rs`

Implements reverse proxy for HTTP requests and WebSocket upgrades. Attaches per-fort JWT to outgoing requests. Handles 401 → refresh → retry.

This task is the core proxy logic extracted from the existing Tauri `proxy.rs` (200 lines) and Go `bff.go` (134 lines). The implementation uses `reqwest` for HTTP and `tokio-tungstenite` for WebSocket bidirectional piping.

**Step 1: Implement proxy module**

Key functions:
- `forward_http(client, service_url, request, token) -> Response`
- `forward_ws(service_url, request, token) -> Result<()>` (bidirectional pipe)

**Step 2: Test with a mock HTTP server**

Use `axum` in tests to spin up a mock service, verify proxy forwards correctly.

**Step 3: Commit**

```bash
git commit -m "feat(scope-core): HTTP and WebSocket reverse proxy"
```

---

### Task 6: Service Discovery + Notification Subscriber

**Files:**
- Create: `crates/scope-core/src/infra/discovery/mod.rs`
- Create: `crates/scope-core/src/infra/discovery/notifications.rs`
- Modify: `crates/scope-core/src/infra/mod.rs`

**Service discovery:** Polls `/ui/health` on configured service URLs. Parses manifests. Maintains `Vec<TrackedService>` behind `RwLock`. Background polling task.

**Notification subscriber:** For each service with `notification_path`, opens a WS connection. Receives pre-classified notifications. Applies user preferences via `Store`. Calls a provided callback for real-time delivery.

**Step 1: Implement ServiceDiscovery**

Key functions:
- `probe_all(fort, client) -> Vec<TrackedService>`
- `start_polling(fort, client, interval) -> JoinHandle`
- `services() -> Vec<TrackedService>` (snapshot under read lock)

**Step 2: Implement NotificationSubscriber**

Key functions:
- `subscribe(service_name, ws_url, token, store, callback) -> JoinHandle`
- The callback is `Box<dyn Fn(Notification) + Send + Sync>` — consumer provides it

**Step 3: Test discovery with mock /ui/health endpoint**

**Step 4: Commit**

```bash
git commit -m "feat(scope-core): service discovery and notification subscriber"
```

---

### Task 7: Config Parsing

**Files:**
- Create: `crates/scope-core/src/config/mod.rs`

Reads YAML config from XDG path. Same format as the old Go BFF config.

```rust
use serde::Deserialize;
use crate::domain::{Fort, ServiceConfig};

#[derive(Debug, Deserialize)]
pub struct Config {
    #[serde(default = "default_listen")]
    pub listen: String,
    #[serde(default = "default_database")]
    pub database: String,
    #[serde(default)]
    pub forts: std::collections::HashMap<String, FortYaml>,
}

#[derive(Debug, Deserialize)]
pub struct FortYaml {
    #[serde(default)]
    pub local: bool,
    pub gateway: Option<String>,
    #[serde(default)]
    pub services: Vec<ServiceYaml>,
}

#[derive(Debug, Deserialize)]
pub struct ServiceYaml {
    pub url: String,
}

fn default_listen() -> String { "127.0.0.1:16100".into() }
fn default_database() -> String {
    directories::ProjectDirs::from("dev", "workfort", "scope")
        .map(|d| d.data_dir().join("scope.db").to_string_lossy().into_owned())
        .unwrap_or_else(|| "scope.db".into())
}

impl Config {
    pub fn load() -> Result<Self, crate::Error> {
        let config_dir = directories::ProjectDirs::from("dev", "workfort", "scope")
            .map(|d| d.config_dir().to_path_buf())
            .unwrap_or_else(|| std::path::PathBuf::from("."));

        let path = config_dir.join("config.yaml");
        if !path.exists() {
            return Ok(Config {
                listen: default_listen(),
                database: default_database(),
                forts: Default::default(),
            });
        }

        let contents = std::fs::read_to_string(&path)
            .map_err(|e| crate::Error::Config(format!("read {}: {e}", path.display())))?;
        serde_yaml::from_str(&contents)
            .map_err(|e| crate::Error::Config(format!("parse {}: {e}", path.display())))
    }

    pub fn into_forts(self) -> Vec<Fort> {
        self.forts.into_iter().map(|(name, f)| Fort {
            name,
            local: f.local,
            gateway: f.gateway,
            services: f.services.into_iter().map(|s| ServiceConfig { url: s.url }).collect(),
        }).collect()
    }
}
```

**Step 1: Implement and test config parsing**

**Step 2: Commit**

```bash
git commit -m "feat(scope-core): YAML config parsing with XDG paths"
```

---

## Phase 4: scope-server (axum)

### Task 8: Basic axum Server with API Endpoints

**Files:**
- Modify: `crates/scope-server/src/main.rs`
- Create: `crates/scope-server/src/routes/mod.rs`
- Create: `crates/scope-server/src/routes/api.rs`
- Create: `crates/scope-server/src/state.rs`

Wire up scope-core to axum routes. Start with non-proxy endpoints: `/api/forts`, `/api/session`, `/api/services`, `/api/notifications`, `/api/preferences`.

**Step 1: Create shared AppState**

```rust
// crates/scope-server/src/state.rs
use std::sync::Arc;
use scope_core::domain::Store;
use scope_core::infra::discovery::ServiceDiscovery;

pub struct AppState {
    pub store: Arc<dyn Store>,
    pub discovery: Arc<ServiceDiscovery>,
}
```

**Step 2: Implement API routes**

Each route is a thin axum handler that calls scope-core. Example:

```rust
async fn list_forts(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    match state.store.list_forts().await {
        Ok(forts) => Json(forts).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}
```

**Step 3: Wire up main.rs with config, store, discovery, and routes**

**Step 4: Test manually**

```bash
cd ~/Work/WorkFort/scope/lead && cargo run -p scope-server
curl http://127.0.0.1:16100/api/forts
```

**Step 5: Commit**

```bash
git commit -m "feat(scope-server): axum server with API endpoints"
```

---

### Task 9: Proxy Routes + SPA Fallback

**Files:**
- Create: `crates/scope-server/src/routes/proxy.rs`
- Create: `crates/scope-server/src/routes/spa.rs`
- Modify: `crates/scope-server/src/main.rs`

Add `/forts/{fort}/api/*` HTTP proxy, `/forts/{fort}/ws/*` WS proxy, and SPA fallback. These use scope-core's `ProxyHandler`.

**Step 1: Implement proxy route handler**

Extracts fort name from path, looks up token, delegates to scope-core proxy.

**Step 2: Implement SPA fallback**

Serves shell dist/ for non-API paths (embedded mode). Uses `tower-http::services::ServeDir`.

**Step 3: Commit**

```bash
git commit -m "feat(scope-server): proxy routes and SPA fallback"
```

---

### Task 10: Shell WebSocket

**Files:**
- Create: `crates/scope-server/src/routes/shell_ws.rs`
- Modify: `crates/scope-server/src/main.rs`

Single WS endpoint at `/ws/shell`. Pushes notifications, service state changes, and version updates to connected browsers.

**Step 1: Implement shell WS handler**

Uses axum's WebSocket upgrade. Maintains a broadcast channel for pushing events. Notification subscriber callback writes to this channel.

**Step 2: Commit**

```bash
git commit -m "feat(scope-server): shell WebSocket for real-time push"
```

---

## Phase 5: Tauri Refactor

### Task 11: Refactor Tauri to Use scope-core

**Files:**
- Modify: `src-tauri/src/lib.rs` — replace inline proxy/token logic with scope-core
- Modify: `src-tauri/src/proxy.rs` — thin wrapper around scope-core proxy
- Modify: `src-tauri/src/auth.rs` — thin wrapper around scope-core session
- Modify: `src-tauri/Cargo.toml` — add `tauri-plugin-notification`

**Step 1: Replace token store with scope-core's**

The in-memory `HashMap<String, FortTokens>` in `proxy.rs` becomes scope-core's `TokenStore`.

**Step 2: Replace proxy logic**

The custom protocol handler delegates to scope-core's `ProxyHandler` instead of inline reqwest calls.

**Step 3: Add notification commands**

New Tauri commands wrapping scope-core: `get_notifications`, `mark_read`, `get_preference`, `set_preference`.

**Step 4: Wire up native notifications**

Add `tauri-plugin-notification`. The notification subscriber callback fires both webview events and native OS notifications.

**Step 5: Verify Tauri builds**

```bash
cd ~/Work/WorkFort/scope/lead && cargo tauri build --debug
```

**Step 6: Commit**

```bash
git commit -m "feat(workfort-scope): refactor Tauri to use scope-core"
```

---

## Phase 6: Shell SPA Updates

### Task 12: Framework-Agnostic Service Mount

**Files:**
- Modify: `web/shell/src/lib/remotes.ts` — update `ServiceModule` interface
- Modify: `web/shell/src/components/service-mount.tsx` — imperative mount path

Replace SolidJS-specific `<Dynamic>` with framework-agnostic `mount`/`unmount` contract. Add `ImperativeMount` component for non-Solid remotes.

**Step 1: Update ServiceModule interface**

```typescript
export interface ServiceModule {
  mount(el: HTMLElement, props: { connected: boolean }): void;
  unmount(el: HTMLElement): void;
  manifest: {
    name: string;
    label: string;
    route: string;
    display: 'nav' | 'menu';
  };
  mountSidebar?(el: HTMLElement): void;
  unmountSidebar?(el: HTMLElement): void;
}
```

**Step 2: Implement ImperativeMount in service-mount.tsx**

**Step 3: Commit**

```bash
git commit -m "feat(shell): framework-agnostic service module contract"
```

---

### Task 13: Update Sharkfin Entry Point

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/index.tsx` — switch from `default` export to `mount`/`unmount`

Convert Sharkfin's MF remote from SolidJS component export to framework-agnostic mount/unmount pattern.

```typescript
import { render } from 'solid-js/web';

const roots = new WeakMap<HTMLElement, () => void>();

export function mount(el: HTMLElement, props: { connected: boolean }) {
  const dispose = render(() => <SharkfinApp connected={props.connected} />, el);
  roots.set(el, dispose);
}

export function unmount(el: HTMLElement) {
  const dispose = roots.get(el);
  if (dispose) {
    dispose();
    roots.delete(el);
  }
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
  display: 'nav' as const,
};
```

**Step 1: Update entry point**

**Step 2: Build and verify it still loads in shell**

```bash
cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build
```

**Step 3: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git commit -m "feat(web): switch to framework-agnostic mount/unmount contract"
```

---

### Task 14: Shell Notification UI + Menu Display

**Files:**
- Modify: `web/shell/src/components/nav-bar.tsx` — add bell icon, render menu-type services in hamburger
- Create: `web/shell/src/components/notification-bell.tsx` — badge count + dropdown
- Create: `web/shell/src/stores/notifications.ts` — notification state from shell WS
- Modify: `web/shell/src/stores/services.ts` — switch from polling to shell WS

**Step 1: Create notification store**

Receives events from shell WS (`/ws/shell` or Tauri events). Maintains notification list and unread count.

**Step 2: Create notification bell component**

Bell icon in nav bar with unread badge. Clicking opens dropdown showing recent notifications. Each notification is clickable (navigates to `route`).

**Step 3: Update nav-bar to render menu-type services in hamburger**

Services with `display: "menu"` appear as links in the hamburger menu instead of tabs in the nav bar.

**Step 4: Switch services store from polling to WS**

Replace the 30-second polling interval with real-time updates from the shell WS.

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git commit -m "feat(shell): notification bell, menu display type, shell WS"
```

---

## Phase 7: Integration

### Task 15: Sharkfin /notifications/subscribe Endpoint

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/pkg/daemon/ws_handler.go` or new file — add notification WS endpoint
- Modify: `~/Work/WorkFort/sharkfin/lead/pkg/daemon/ui_handler.go` — add `notification_path` to health manifest

Add a WebSocket endpoint at `/notifications/subscribe` that pushes pre-classified notifications. Uses Sharkfin's existing event bus — subscribe to message events, classify mentions/DMs as `active`, regular messages as `passive`, emit to the WS.

**Step 1: Add notification_path to health manifest**

```go
json.NewEncoder(w).Encode(uiHealthResponse{
    Status:           "ok",
    Name:             "sharkfin",
    Label:            "Chat",
    Route:            "/chat",
    WSPaths:          []string{"/ws", "/presence"},
    NotificationPath: "/notifications/subscribe",
})
```

**Step 2: Implement /notifications/subscribe handler**

Authenticates via JWT (same as /ws). Subscribes to the event bus. For each message event, classifies and sends:

```json
{"title": "New message in #general", "body": "@admin mentioned you", "urgency": "active", "route": "/chat"}
```

**Step 3: Test with wscat or similar**

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git commit -m "feat: add /notifications/subscribe WebSocket endpoint"
```

---

### Task 16: End-to-End Verification

Manual integration test. Verify the full flow:

1. Start Passport VM
2. Start Sharkfin daemon (with `notification_path` in health)
3. Start `scope-server` with config pointing to both services
4. Open browser to `http://127.0.0.1:16100`
5. Sign in as admin
6. Verify services appear in nav (Chat) and hamburger menu (Admin, once Passport UI is built)
7. Open a second browser tab, send a message in Sharkfin
8. Verify notification appears in the first tab's bell icon
9. Verify `scope-server` logs show notification subscriber connected to Sharkfin

---

## Summary

| Phase | Tasks | What it builds |
|---|---|---|
| 1 | 1-2 | Workspace + domain types |
| 2 | 3-4 | SQLite + Postgres store adapters |
| 3 | 5-7 | Proxy, discovery, notification subscriber, config |
| 4 | 8-10 | scope-server (axum) |
| 5 | 11 | Tauri refactor |
| 6 | 12-14 | Shell SPA (service contract, notifications, menu) |
| 7 | 15-16 | Sharkfin notification endpoint + integration test |
