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
- A `table.record.changed` trigger node that accepts an `rhistory_db_table_record_id_timestamp` key and exposes the decoded row change.
- Workflow JavaScript editing with JSON editors for GitHub Actions-style secrets and variables.
- Git-managed artifacts live under `repository.path`: table metadata at `metadata/main.yml`, workflow JavaScript at `workflow/<database>/<workflow>.js`, and form JavaScript at `form/<database>/<form>.js`.
- A form JavaScript runtime that requires `function render(api, root)` to render controls with `field` configs and return `{ table }`.
- Form submissions send input JSON; the backend executes the form JavaScript to resolve the target table and field-bound controls before writing records.
- Runtime data is rooted at `data.path`: `system.sqlite`, `leveldb`, and per-database `<database>.sqlite` files are derived from that directory instead of being configured separately.

## Development Rules

- This project is in active development; breaking changes are allowed.
- Do not preserve backward-compatible or legacy behavior unless explicitly requested.
- Data/schema upgrades do not get compatibility code or runtime migrations. During demo development, delete the old generated data manually, including individual SQLite files, the LevelDB directory, or the whole `data/` directory.
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
  -v autable-data:/data \
  -v autable-repository:/repository \
  ghcr.io/autable/autable:latest
```

The container uses `/etc/autable/config.yml` by default, listens on `0.0.0.0:8080`, stores runtime data under `/data`, and stores user-authored metadata/workflows/forms under `/repository`.

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

SQLite files and LevelDB directories must be backed up by users/operators. The project will keep generated data out of git while keeping user-authored metadata, workflows, forms, and config files in git.
