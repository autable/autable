# codetable

codetable is a code-first alternative to multidimensional tables. The project is not a no-code or low-code builder: tables, forms, and workflows are stored as code-managed artifacts so AI-assisted development can work directly with the source of truth.

## Direction

- Go backend with SQLite for system/application data.
- LevelDB-compatible key/value storage for removable history/log data.
- TypeScript React frontend for table, workflow, and form surfaces.
- User-managed repository containing `config.yml`, table metadata, workflow JavaScript, and form JavaScript.

## Current Slice

This repository currently contains the backend core primitives:

- `config.yml` loading and validation, including OIDC settings.
- YAML metadata for databases and tables, including soft-deleted fields.
- Hidden, auto-incrementing `record_id` handling.
- Field-level permissions: none, read, write.
- User identity model with email fallback for password and OIDC accounts.
- History key generation and prefix scanning for row/workflow history.
- Stateless workflow node interface definitions.

## Development

```sh
go test ./...
```

Run the API server with git-managed config and metadata:

```sh
go run ./cmd/codetable -config examples/config.yml -metadata examples/metadata/main.yml
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
```

SQLite files and LevelDB directories must be backed up by users/operators. The project will keep generated data out of git while keeping user-authored metadata, workflows, forms, and config files in git.
