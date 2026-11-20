# Working on this autable workflow

This project is a workflow of an autable server. `autable.workflow.json`
names the database and the workflow; `autablectl` (signed in with
`autablectl login`, as an admin) builds it, uploads it, and publishes it.

## Loop

1. `npm install` once.
2. Edit `src/`. `npm run typecheck` must pass; `npm run build` runs it and
   bundles `src/index.ts` with esbuild into `dist/workflow.js`.
3. `autablectl workflow push -m "what changed"` builds and uploads a new
   version. Pushing never changes what runs.
4. `autablectl workflow publish <database> <name> <version>` (or push with
   `--publish`) makes the version the one that runs; the server reads the
   triggers then and rejects invalid ones. `autablectl workflow rollback
   <database> <name>` publishes the version before the current one.
5. Try it: `autablectl workflow run <database> <name> --trigger start
   --input '{"note":"hi"}' --wait` prints the finished run; `autablectl
   workflow runs <database> <name>` and `autablectl workflow run-log
   <database> <name> <run id>` show the history with logs and errors.
   Messages and writes a run makes are real.

`autablectl workflow versions <database> <name>` shows versions, triggers
(with webhook URLs), settings, secret names, and vars. `autablectl workflow
pull <database> <name> <dir>` downloads the source of any version.
`autablectl workflow disable` stops the triggers; `enable` starts them again.

## How a workflow runs

- `src/index.ts` exports `triggers` (named declarations built with
  `schedule`, `recordChanged`, `webhook`, `manual`) and `run(event)`. Type
  the event as `TriggerEvent<typeof triggers>` and `switch` on
  `event.trigger`; each case sees its trigger's fields (`scheduledAt`,
  `change`, `request`, `input`).
- A run is one call of `run`, from start to finish, in a fresh runtime.
  Nothing is kept between runs except what the run writes: table rows,
  files, its log, and its output (the returned value). The time limit is
  the workflow's timeout, 10 minutes by default.
- The runtime is neither Node nor a browser. Use the SDK's `fetch`,
  `crypto`, `base64`/`hex`/`utf8`, plus the standard globals in
  `src/autable/globals.d.ts` (`TextEncoder`, `URL`, `setTimeout`,
  `console`, ...). Plain-JavaScript npm packages bundle fine; packages that
  need Node built-ins do not.
- The workflow reads and writes tables as its own subject,
  `workflow:<id>`, with the grants an admin gave it, not those of whoever
  started it. A call outside them rejects with a `PermissionError`
  (`isPermissionError(error)`).

## Long processes: state lives in rows

A run cannot wait for hours or days. Keep the state of a process that
spans time (an approval, a multi-stage order) in table rows and let
triggers move it on:

- **Approvals in another system** (IM, ERP): a `schedule` trigger polls the
  other system every few minutes for the requests whose rows are still
  pending, and writes each result into its row.
- **Stages**: a `recordChanged` trigger with `fields: ["<state field>"]`
  runs the workflow again whenever a row's state changes; the run reads
  `event.change` (values, `diff`, `changedFields`, `actor`), does that
  stage's work, and writes the next state. Writes the workflow makes
  itself do not fire it again (`includeOwnWrites` defaults to false).
- **Idempotence**: check the row's current state before acting, and use
  `upsert` or a unique field where a repeated run would create duplicates.

## Triggers

- `schedule({ cron, timezone? })`: five-field cron, IANA timezone
  (default UTC).
- `recordChanged({ table, operations?, fields?, includeOwnWrites? })`.
- `webhook({ path })` answers `ANY /api/hooks/<database>/<name>/<path>`
  without an autable session: check the caller yourself (for example an
  HMAC over `requestText(event.request)` with `crypto.hmac` and
  `crypto.timingSafeEqual`). What `run` returns is the HTTP response,
  `{ status, headers?, body? }`, within 15 seconds; a webhook run usually
  just records what it received in a row.
- `manual({ fields? })`: started from the web UI (a form built from
  `fields`), `autablectl workflow run`, the API, or an app. Users with a run
  grant may start it.

## Secrets, vars, runners

- Secrets (API keys, signing keys) are set by an admin:
  `autablectl workflow secret set <database> <name> <key>` reads the value
  from stdin or a prompt. Read them with `secrets.require("key")`. Their
  values are never returned by the API; never log them or return them.
- Vars are plain settings: `autablectl workflow var set <database> <name>
  <key> <value>`, read with `vars.get("key")`.
- `fetch(url, { via: "<runner>" })` sends the request through a runner of
  the database, for systems only reachable inside a network.

## The SDK

- `src/autable/` is vendored by `autablectl workflow sdk`: the SDK
  (`index.ts` and the modules it re-exports), `globals.d.ts`, and
  `schema.ts` with the tables and fields of the database as types. Do not
  edit them; run `autablectl workflow sdk` after the tables change or the
  server is upgraded. It also updates `sdk` in `autable.workflow.json`.
- Import everything from `"./autable"`. Its doc comments are the
  reference.
- `npm run build` must produce `dist/workflow.js`. The source pushed with
  each version is what git would commit (or, outside git, everything except
  `node_modules`, `dist`, `.git`, and `.autable`); keep secrets out of it.
