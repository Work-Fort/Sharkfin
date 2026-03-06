# Encrypted S3 Backup Design

## Goal

Full encrypted backup and restore of sharkfin config and database to S3-compatible storage. Backups are backend-portable — export from SQLite, import to Postgres (or vice versa). No incremental backups.

## Commands

```
sharkfin backup export      # dump config + DB, encrypt, upload to S3
sharkfin backup import <key> # download from S3, decrypt, restore
sharkfin backup list        # list backups in the configured bucket
```

## Archive Format

`tar.xz.age` — a tar archive containing `config.yaml` and `data.json`, compressed with xz, encrypted with age using a passphrase.

S3 object key: `sharkfin-backup-{RFC3339-UTC}.tar.xz.age`

Example: `sharkfin-backup-2026-03-06T12:00:00Z.tar.xz.age`

## Data Serialization

Data is queried through the `domain.Store` interface and serialized as JSON. All references use natural keys (usernames, channel names) rather than database IDs, making backups portable across backends.

On import, new IDs are auto-assigned by the target database.

### `data.json` Schema

```json
{
  "version": 1,
  "exported_at": "2026-03-06T12:00:00Z",
  "users": [
    {"username": "alice", "password": "", "role": "admin", "type": "user"}
  ],
  "channels": [
    {"name": "general", "public": true, "type": "channel"}
  ],
  "channel_members": {
    "general": ["alice", "bob"]
  },
  "messages": [
    {
      "channel": "general",
      "from": "alice",
      "body": "hello",
      "thread_id": null,
      "mentions": ["bob"],
      "created_at": "2026-03-06T12:00:00Z"
    }
  ],
  "roles": [
    {"name": "admin", "built_in": true}
  ],
  "role_permissions": {
    "admin": ["send_message", "manage_roles"]
  },
  "settings": {
    "key": "value"
  },
  "dms": [
    {"user1": "alice", "user2": "bob", "channel_name": "dm-alice-bob"}
  ],
  "read_cursors": [
    {"channel": "general", "username": "alice", "last_message_from": "bob", "last_message_body": "hello"}
  ]
}
```

Read cursors reference messages by content (channel + from + body + created_at) since IDs are not portable. On import, cursors are matched to the corresponding message ID in the target database.

## Configuration

Viper keys in `config.yaml` under `backup:` prefix:

```yaml
backup:
  s3-bucket: "my-bucket"
  s3-region: "us-east-1"
  s3-endpoint: ""           # optional, for MinIO/R2/Cloudflare etc.
  s3-access-key: "AKIA..."
  s3-secret-key: "secret..."
```

Available as CLI flags (`--s3-bucket`, `--s3-region`, `--s3-endpoint`, `--s3-access-key`, `--s3-secret-key`) and env vars (`SHARKFIN_BACKUP_S3_BUCKET`, etc.).

The encryption passphrase is provided via `--passphrase` flag or `SHARKFIN_BACKUP_PASSPHRASE` env var. Never stored in config file. Commands prompt interactively if not provided.

## Command Details

### `sharkfin backup export`

1. Read passphrase from flag/env or prompt
2. Open Store via `infra.Open(dsn)`
3. Query all data through domain.Store interfaces
4. Serialize to `data.json`
5. Read `config.yaml` from config dir
6. Create tar archive containing both files
7. Compress with xz
8. Encrypt with age passphrase
9. Upload to S3 with timestamped key
10. Print: key, size, timestamp

Flags: `--db`, `--passphrase`, S3 flags

### `sharkfin backup import <key>`

1. Read passphrase from flag/env or prompt
2. Download object from S3 by key
3. Decrypt with age → decompress xz → extract tar
4. Parse `data.json`, validate version field
5. Open Store (target database)
6. Check database is empty; require `--force` to overwrite non-empty DB
7. Insert in order: users → roles → role_permissions → channels → channel_members → messages → read_cursors → settings → DMs
8. Optionally restore `config.yaml` with `--restore-config` flag

Flags: `--db`, `--passphrase`, `--force`, `--restore-config`, S3 flags

### `sharkfin backup list`

1. List objects in bucket with `sharkfin-backup-` prefix
2. Print table: key, size (human-readable), last modified

Flags: S3 flags

## Dependencies

New:
- `filippo.io/age` — passphrase-based encryption
- `github.com/ulikunitz/xz` — pure Go xz compression
- `github.com/aws/aws-sdk-go-v2` + `config`, `credentials`, `service/s3` — S3 client

## File Structure

```
cmd/backup/backup.go       — cobra command tree (export, import, list)
pkg/backup/export.go       — export orchestration
pkg/backup/import.go       — import orchestration
pkg/backup/s3.go           — S3 upload/download/list
pkg/backup/archive.go      — tar+xz+age encrypt/decrypt pipeline
pkg/backup/data.go         — data.json types, Store→JSON, JSON→Store
```

## Error Handling

- **Wrong passphrase**: age returns a clear error on decrypt failure; surface it
- **Missing S3 config**: fail early with message naming the missing field
- **Non-empty DB on import**: refuse unless `--force`; with `--force`, wipe and reimport
- **Partial import failure**: best-effort transaction wrapping; on error, report which step failed
- **S3 connectivity**: AWS SDK default timeout + retry behavior

## Backend Portability

Because data flows through the `domain.Store` interface with natural keys:

- Export from SQLite → Import to Postgres: works
- Export from Postgres → Import to SQLite: works
- This enables zero-downtime backend migration

## Testing

- Unit: archive pipeline roundtrip (tar → xz → age → decrypt → decompress → untar)
- Unit: data serialization roundtrip (Store → JSON → Store against `:memory:` SQLite)
- E2E: export from daemon, import to fresh daemon, verify data matches via MCP queries
