# autable

autable is a code-first alternative to multidimensional tables. The project is not a no-code or low-code builder: tables, forms, and workflows are stored as code-managed artifacts so AI-assisted development can work directly with the source of truth.

## Direction

- Go backend with SQLite for system/application data.
- LevelDB-compatible key/value storage for removable history/log data.
- TypeScript React frontend for table, workflow, and form surfaces.
- User-managed repository containing `config.yml`, table metadata, workflow JavaScript, and form JavaScript.

## Current Slice

This repository currently contains the backend core primitives:

- `config.yml` loading and validation, including password and OIDC auth toggles under `auth`.
- YAML metadata for databases and tables, including soft-deleted fields.
- Authenticated database/table creation that writes metadata YAML and grants database/table owner permissions.
- Hidden, auto-incrementing `ct_record_id` handling for user table storage.
- Per-metadata-database SQLite row persistence through GORM.
- Row create/update history written to LevelDB-compatible storage.
- Table views with composable `base_view`, filters, and sorts.
- Password registration/login with HttpOnly session cookies.
- OIDC provider listing plus authorization-code callback login with verified ID tokens and HttpOnly state/session cookies.
- Field-level permissions: none, read, write.
- Workflow and form resource permissions using their auto-incrementing IDs.
- User identity model with email fallback for password and OIDC accounts.
- History key generation and prefix scanning for row/workflow history.
- Stateless workflow node interface definitions.
- Workflow node metadata API and frontend node catalog for available stateless nodes and trigger nodes.
- Synchronous JavaScript workflow runs through registered stateless nodes, with each run persisted as `whistory_id_timestamp`.
- Remote runners: node instances can be bound (in the UI, beside instance variables/secrets) to a named `autable-runner` process that connects outbound over WebSocket and executes remote-capable nodes inside another network; a single resettable system token authorizes runners (see `docs/design/remote-runner.md`).
- A `kingdee.purchaseorder.list` node that pages purchase order lines out of Kingdee K3Cloud through a pure-Go WebAPI client (`internal/kingdee`) with request signing matching the official Python SDK.
- A `table.record.changed` trigger node that accepts an `rhistory_db_table_record_id_timestamp` key and exposes the decoded row change.
- Workflow JavaScript editing with JSON editors for GitHub Actions-style secrets and variables.
- Git-managed artifacts live under `repository.path`: table metadata at `metadata/main.yml`, workflow JavaScript at `workflow/<database>/<workflow>.js`, and form JavaScript at `form/<database>/<form>.js`.
- A form JavaScript runtime that requires `function render(api, root)` to render controls with `field` configs and return `{ table }`.
- Form submissions send input JSON; the backend executes the form JavaScript to resolve the target table and field-bound controls before writing records.
- Runtime data is rooted at `data.path`: `system.sqlite`, `leveldb`, and per-database `<database>.sqlite` files are derived from that directory instead of being configured separately.

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

Run the API server with git-managed config. On startup, Autable clones `repository.remote_url` at `repository.remote_branch` into `repository.path` when the path does not exist. After startup, local metadata, workflow, and form changes are committed and pushed to that remote branch; Autable does not pull or rebase remote changes automatically. The server loads metadata from `repository.path/metadata/main.yml` and stores runtime data under `data.path`:

```sh
cp examples/config.example.yml examples/config.yml
# Edit repository.remote_url and repository.remote_branch before starting.
go run ./cmd/autable -config examples/config.yml
```

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

The container uses `/etc/autable/config.yml`, listens on `0.0.0.0:8080`, stores runtime data under `/data`, and stores user-authored metadata/workflows/forms under `/repository`. Mount a config file that sets `repository.remote_url` and `repository.remote_branch`; an empty `/repository` volume is initialized by cloning the remote, or by creating the local branch when the remote repository is empty.

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

Autable can upload scheduled backups to S3-compatible storage. SQLite databases are copied with SQLite's online backup API, so the service does not need to stop while a backup is created. LevelDB history is optional; when `backup.include_leveldb` is enabled, Autable exports a consistent snapshot into a restorable LevelDB directory inside the archive:

```yaml
backup:
  enabled: true
  interval: "24h"
  include_leveldb: false
  tmp_dir: "/tmp/autable-backups"
  s3:
    endpoint: "https://s3.example.com"
    region: "us-east-1"
    bucket: "autable-backups"
    prefix: "prod/autable"
    access_key_id: "..."
    secret_access_key: "..."
    force_path_style: true
```

The project keeps generated runtime data out of git while keeping user-authored metadata, workflows, and forms in the configured repository remote.
