# autable

autable is a code-first alternative to multidimensional tables. The project is not a no-code or low-code builder: tables and workflows are stored as code-managed artifacts, and user interfaces are hosted apps built as ordinary frontend projects, so AI-assisted development can work directly with the source of truth.

autable is admin-gated. The global admins listed under `auth.admins` in the config file are the only people who write code or structure (databases, schema, formulas, views, permissions, workflows, apps); other developers write code elsewhere and deploy it with an admin account. Everyone else gets data access and can open the apps and run the workflows they are granted. See [docs/permissions.md](docs/permissions.md).

## Direction

- Go backend with SQLite for system/application data.
- LevelDB-compatible key/value storage for removable history/log data.
- TypeScript React frontend for table, workflow, and app management surfaces.
- Hosted apps: ordinary frontend projects that autable serves from the same origin as its API (see [docs/design/apps.md](docs/design/apps.md)).
- Git repository receiving table metadata and workflow JavaScript as a backup.

## Current Slice

This repository currently contains the backend core primitives:

- `config.yml` loading and validation, including password and OIDC auth toggles and the `auth.admins` list under `auth`.
- YAML metadata for databases and tables, including soft-deleted fields.
- Database and table creation, restricted to admins, that writes metadata YAML.
- Hidden, auto-incrementing `ct_record_id` handling for user table storage.
- Per-metadata-database SQLite row persistence through GORM.
- Row create/update history written to LevelDB-compatible storage.
- Table views with composable `base_view`, filters, and sorts.
- Password registration/login with HttpOnly session cookies (`__Host-autable_session` when `server.public_url` is https) and cross-site request checks on cookie-authenticated API writes.
- OIDC provider listing plus authorization-code callback login with verified ID tokens and HttpOnly state/session cookies.
- Default-deny data permissions: per-field read/update/create bits, record create/delete, row sets defined by readable views (including `$current_user` views), and row-scoped file access.
- App open and workflow run permissions using their auto-incrementing IDs; everything else about apps and workflows is admin-only.
- User identity model with email fallback for password and OIDC accounts.
- History key generation and prefix scanning for row/workflow history.
- Stateless workflow node interface definitions.
- Workflow node metadata API and frontend node catalog for available stateless nodes and trigger nodes.
- Synchronous JavaScript workflow runs through registered stateless nodes, with each run persisted as `whistory_id_timestamp`.
- Remote runners: node instances can be bound (in the UI, beside instance variables/secrets) to a named `autable-runner` process that connects outbound over WebSocket and executes remote-capable nodes inside another network; a single resettable system token authorizes runners (see `docs/design/remote-runner.md`).
- `autablectl`, a command-line client for scripts and coding agents: browser sign-in issues a bearer session token, and commands read and write tables and rows, and, with an admin account, edit workflows and build, push, and publish hosted apps, including a pull/edit/push loop for workflow scripts with conflict detection (see `docs/cli.md`).
- Hosted apps: immutable app versions (built files plus project source) stored in the system database, published at `/apps/<database>/<name>/`, with a versioned TypeScript SDK served by the server (see `docs/design/apps.md`).
- An ERP integration node that pages purchase order lines through a pure-Go WebAPI client with request signing.
- A `table.record.changed` trigger node that accepts an `rhistory_db_table_record_id_timestamp` key and exposes the decoded row change.
- Workflow JavaScript editing with JSON editors for GitHub Actions-style secrets and variables.
- Git-managed artifacts live under `repository.path`: table metadata at `metadata/main.yml` and workflow JavaScript at `workflow/<database>/<workflow>.js`.
- Runtime data is rooted at `data.path`: `system.sqlite`, `leveldb`, and per-database `<database>.sqlite` files are derived from that directory instead of being configured separately.
- Excel import into tables from the table toolbar: the file is parsed in the browser (SheetJS), users pick the worksheet and columns, missing `string`/`int`/`float` fields are created through the field API (which requires an admin), and rows are written serially through the existing create/upsert row APIs with per-row dedup on a match field (update or skip on duplicates).
- A string field with `options` is an enum; adding `multiple: true` lets it hold several of them at once, stored comma-separated (so an option may not contain a comma). Multi-valued cells are edited as checkboxes in the row panel, and are left out of grid editing because a cell cannot show a set of choices.

## Development Rules

- This project is in active development; breaking changes are allowed.
- Do not preserve backward-compatible or legacy behavior unless explicitly requested.
- The product is live: existing databases must upgrade in place. The system database records a schema version; `migrations[i]` in `internal/systemdb/migrations.go` upgrades version i to i+1, runs unconditionally in a transaction, and may use raw SQL (the no-hand-written-SQL rule does not apply there). Migrations never probe for tables or columns — the version guarantees the starting state, and drift fails loudly. Append new migrations only; never edit or reorder released ones. Never ship a model change AutoMigrate cannot apply to populated tables, such as a new NOT NULL column without a default — that is what migrations are for.
- When changing a contract, update callers, tests, and docs to the new contract and remove the old path.
- Do not add fallback behavior for non-normal paths unless explicitly requested. Required data, metadata, and configuration failures should fail visibly instead of silently degrading to inferred or partial behavior.
- Use the ORM for database access; do not hand-write SQL in application code.
- All system timestamps are millisecond-precision 64-bit Unix timestamps.
- Table fields have no `required` concept; field types are immutable after creation.

## Development

```sh
go test ./...
```

Run the API server with git-managed config. On startup, Autable clones `repository.remote_url` at `repository.remote_branch` into `repository.path` when the path does not exist. After startup, local metadata and workflow changes are committed and pushed to that remote branch; Autable does not pull or rebase remote changes automatically. The server loads metadata from `repository.path/metadata/main.yml` and stores runtime data under `data.path`:

```sh
cp examples/config.example.yml examples/config.yml
# Edit repository.remote_url and repository.remote_branch before starting.
go run ./cmd/autable -config examples/config.yml
```

List at least one admin email under `auth.admins` before starting; with an empty list the server starts but logs a warning, and nobody can create databases or change schema, permissions, workflows, or apps:

```yaml
auth:
  admins:
    - "admin@example.com"
  password:
    enabled: true
```

Admins are matched by email, and password registration is open whenever `auth.password.enabled` is true: whoever registers an admin email first becomes that admin. Register the admin accounts before or as the config is deployed, or disable password auth and sign in through OIDC, whose provider should verify email addresses.

Build a single Go binary with the frontend embedded:

```sh
cd web
npm install
cd ..
./scripts/embed-web.sh
go build -o autable ./cmd/autable
```

The binary serves the API and frontend on the same `server.address`.

Run the published Docker image:

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$PWD/config.yml:/etc/autable/config.yml:ro" \
  -v autable-data:/data \
  -v autable-repository:/repository \
  ghcr.io/autable/autable:latest
```

The container uses `/etc/autable/config.yml`, listens on `0.0.0.0:8080`, stores runtime data under `/data` (including the system database, which holds hosted app versions), and stores user-authored metadata/workflows under `/repository`. Mount a config file that sets `repository.remote_url` and `repository.remote_branch`; an empty `/repository` volume is initialized by cloning the remote, or by creating the local branch when the remote repository is empty.

To debug memory or CPU in Docker, enable pprof in `config.yml`:

```yaml
debug:
  pprof_address: "0.0.0.0:6060"
```

Expose it only on localhost:

```sh
docker run --rm \
  -p 8080:8080 \
  -p 127.0.0.1:6060:6060 \
  -v "$PWD/config.yml:/etc/autable/config.yml:ro" \
  -v autable-data:/data \
  -v autable-repository:/repository \
  ghcr.io/autable/autable:latest
```

Then inspect profiles from the host:

```sh
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
curl http://127.0.0.1:6060/debug/pprof/goroutine?debug=2
```

Run the frontend:

```sh
cd web
npm install
npm run dev
```

Frontend verification:

```sh
cd web
npm test
npm run build
npm run e2e
```

Autable uses one shared S3-compatible bucket, configured once under `s3.connection`; each feature stores its objects under its own directory from `s3.directories`. Scheduled backups go under `s3.directories.backup` and user-uploaded files (the `file` field type) under `s3.directories.files`, where each upload lives at `<files directory>/<file id>/<file name>`. SQLite databases are copied with SQLite's online backup API, so the service does not need to stop while a backup is created. LevelDB history is optional; when `backup.include_leveldb` is enabled, Autable exports a consistent snapshot into a restorable LevelDB directory inside the archive:

```yaml
s3:
  connection:
    endpoint: "https://s3.example.com"
    region: "us-east-1"
    bucket: "autable"
    access_key_id: "..."
    secret_access_key: "..."
    force_path_style: true
  directories:
    backup: "backup"
    files: "files"

files:
  max_upload_size_mb: 20

backup:
  enabled: true
  interval: "24h"
  include_leveldb: false
  tmp_dir: "/tmp/autable-backups"
```

The project keeps generated runtime data out of git while keeping user-authored metadata and workflows in the configured repository remote. Hosted app versions, including their source, live in the system database and are covered by its backups.
