# Workflows v2: code projects on platform primitives

Status: accepted; replaces the node-based workflows. No compatibility:
existing workflows are rewritten on the new model.

## Why

The old model was one JavaScript file with `instances`, `trigger` and
`run`, evaluated synchronously in goja. The only way to reach the outside
world was a **node**: Go code compiled into autable. That had three costs:

- **Every integration was a release.** Even one HTTP call or signature
  needed a new node, a new build and an upgrade.
- **Triggers were hard-coded** in the API server. The webhook body format
  was fixed, so third-party callbacks could not be received.
- **There was no versioning, rollback or shared code.**

## Model

A workflow is built and versioned exactly like an app (`apps.md`):

1. **Written elsewhere.** It is a TypeScript project, written by admins
   or by developers whose code an admin deploys.
2. **Built by `autablectl`.** The CLI bundles the project with esbuild into
   one script.
3. **Uploaded as an immutable version.** The upload carries the source
   and a manifest stating the SDK version.
4. **Published.** Publishing selects the version that runs. Rolling back
   is publishing an older version.

Only admins create, push, publish, delete, read or configure workflows.
Users with a `workflow` grant (level 1 = run) may start the `manual`
trigger, nothing else.

autable gives the code a small, versioned set of **primitives**.
Integrations with outside systems (IM, ERP, code hosting, payment) are
ordinary TypeScript built on `fetch` and `crypto`, and live with the
deployment, never in autable. There are no nodes. The deployment shares
an integration in one of two ways:

- **Shared code in its development repository** (a `lib/` beside its
  workflows), bundled into every workflow that imports it; fixing it
  means rebuilding and pushing those workflows. See "Shared code in the
  development repository" below.
- **Libraries** (`@lib/<name>`): instance-wide modules that admins push
  and publish like workflows, and that every run loads in their
  published version, so publishing a fix reaches every importing
  workflow at its next run without pushing the workflows and without an
  autable release. See "Libraries" below.

Workflows are **stateless**: a run is one execution of `run(event)` from
start to finish. Anything that must outlive a run lives in table rows.

### Project layout

```
autable.workflow.json   {"database","name","title","sdk"}
src/index.ts            exports `triggers` and `run`
src/autable/            vendored SDK + schema.ts (autablectl workflow sdk)
dist/workflow.js        esbuild output (IIFE assigning globalThis.__workflow)
```

The bundle is the same gzip tar format as apps: manifest, `dist/`,
`source/`. `dist/workflow.js` is required. Workflow names follow the app
name rule, `^[a-z0-9][a-z0-9-]{0,62}$`.

### Shared code in the development repository

Integrations live with the deployment, usually in a shared `lib/` of the
git repository that holds its workflows and apps (`repo/lib/`,
`repo/workflows/<name>/`, `repo/apps/<name>/`). Workflows import it by
relative path; esbuild bundles it into each workflow, so fixing shared code
reaches a workflow when that workflow is rebuilt and pushed. Shared code
imports the SDK as `@autable/workflow`, which the template maps to the
project's `src/autable/index.ts` (`paths` in `tsconfig.json`, `alias` in
`build.mjs`), so it uses the SDK of whichever workflow bundles it. It takes
credentials from its caller (read with `secrets.require` in the workflow)
rather than reading fixed secret names itself.

The source uploaded with a version must still build, so `build.mjs` writes
the bundle's inputs from esbuild's metafile to
`.autable/build-inputs.json`, and `autablectl workflow push` adds the inputs
outside the project (never `node_modules`) to `source/` at their paths from
the git work-tree root, the project going to `source/<root>/` and the
uploaded manifest recording `"root"`. Without outside inputs the layout is
unchanged. Outside a git work tree a build that used outside files is
refused. `workflow pull` recreates the slice. Details: "Shared code in a
development repository" in `docs/cli.md`.

### Libraries

A library is a TypeScript project like a workflow, but instance-wide (it
belongs to no database) and without triggers. Admins push, publish, roll
back, pull and delete it (`autablectl lib ...`, the Libraries page in
Manage).

```
autable.lib.json        {"name","title","sdk"}
src/index.ts            the public API: its exports are what importers get
src/autable/            vendored workflow SDK, schema.ts without tables, libs/*.d.ts
dist/lib.js             esbuild output (IIFE assigning __lib, the module's exports)
dist/index.d.ts         the declarations importers vendor (declare module "@lib/<name>")
```

The bundle is the usual gzip tar (manifest `autable.lib.json`,
`dist/lib.js` and `dist/index.d.ts` required, `source/`). Library names
follow the workflow name rule.

**Importing.** Workflow and library code writes `import { postJSON } from
"@lib/http"`. The templates' `build.mjs` keeps such imports out of the
bundle: an esbuild plugin turns each into a CommonJS stub
`module.exports = __autable_lib("@lib/http")`, so named imports work.
`autablectl workflow sdk` (and `init`, `lib sdk`) vendors the declarations
of every published library into `src/autable/libs/<name>.d.ts`, so the
imports type-check. The runtime defines `__autable_lib` but no global
`require`: bundled npm packages that detect Node with `typeof require`
keep their browser paths.

**Linking at run time.** `__autable_lib("@lib/<name>")` loads the
library's **published** version (looked up when the run first imports
it), evaluates `dist/lib.js` in the run's own runtime and returns its
exports. So a library uses the same SDK primitives as the workflow and
its `secrets` and `vars` are the calling workflow's; it has no grants of
its own (table calls act as the workflow). Each library is evaluated at
most once per run and shared by every importer in it; libraries may
import libraries. An import cycle, a missing or unpublished library, or a
library whose SDK this server no longer runs fails the import with an
error naming the library (a thrown `Error` the code may catch; uncaught,
it fails the run). Each run records the libraries it loaded with their
versions (`libraries: [{name, version}]` on the run).

**Publishing.** Publishing a workflow version (also with `push
--publish`) reads the library imports from `dist/workflow.js` (the
`__autable_lib("@lib/<name>")` calls) and fails with `422` when one is
missing, unpublished or unsupported; the imports are stored with the
version (`libraries`). Publishing a library version checks its imports the
same way, refuses a library importing itself, and refuses imports that
would make published libraries import each other in a cycle. A pinned
workflow version keeps running with whatever library versions are
published at its runs: compatibility is the library author's job (keep
exports backward compatible; publish an incompatible API under a new
name). Rolling back a library is publishing an older version and takes
effect at the next run.

**Deleting** a library is refused (`409`, naming them) while a published
workflow version or published library version imports it. Compiled
library versions are cached in memory by version.

**Mirror.** Library versions are mirrored into the git backup under
`lib/<name>/` like workflows.

### Code shape

```ts
import { schedule, recordChanged, webhook, manual, fetch, secrets, tables, log } from "./autable";

export const triggers = {
  sync: schedule({ cron: "*/10 * * * *", timezone: "Asia/Shanghai" }),
  changed: recordChanged({ table: "订单", operations: ["update"], fields: ["状态"] }),
  ping: webhook({ path: "ping" }),
  start: manual({ fields: [{ name: "orderId", label: "Order", type: "number" }] }),
};

export async function run(event: TriggerEvent<typeof triggers>) {
  switch (event.trigger) {
    case "sync":
      // poll an external system and write what changed into rows
      break;
    case "changed":
      // react to the row change; event.change.changedFields, event.change.actor
      break;
    case "ping":
      return { status: 200, body: "pong" };   // webhook runs answer the HTTP request
    case "start":
      await tables("订单").update(event.input.orderId, { 状态: "submitted" });
  }
}
```

- **Triggers** are read once, when a version is published: the bundle is
  evaluated and the `triggers` export is stored. Publishing fails (`422`)
  when the declarations are invalid.
- **Trigger kinds:**
  - `schedule`: cron with seconds omitted (5 fields), plus an IANA
    timezone (default UTC). Missed minutes (server down) are not caught up.
  - `recordChanged`: `table`, optional `operations` (default create,
    update and delete), optional `fields` (fire only when one of them
    changed) and `includeOwnWrites` (default `false`: changes written by
    this workflow's own runs do not fire it, so a workflow that updates
    the row it reacts to cannot loop on itself). The event carries the
    row change with its `actor` (a user, or `workflow:<id>`) and
    `changedFields`, with values and before/after diffs filtered by the
    workflow subject's field grants.
  - `webhook`: a path; the run gets the raw request and its return value
    is the HTTP response.
  - `manual`: optional `fields` list of `{name, label, type: "string" |
    "number" | "boolean"}` used for the input form; started from the
    API, the CLI or an app.
- **`run`** receives `{ trigger, runId, ... }` with the trigger-specific
  fields: `scheduledAt`, `change`, `request` or `input`.

The exact declaration objects, event shapes and the host functions the
SDK is built on are specified in
[workflows-host-api.md](workflows-host-api.md).

### Primitives (SDK 1.0)

| Primitive | Behaviour |
|---|---|
| `fetch(url, init?)` | `method`, `headers`, `body` (string, bytes or a JSON-able value with `json:`), `timeoutMs` (default 30 s), `via` (runner name). Returns `{ status, headers, text(), json(), bytes() }`. Responses are capped at 20 MiB. With `via`, the request is performed by that runner. |
| `crypto` | `hash(alg, data)` for sha1, sha256, sha512 and md5; `hmac(alg, key, data)`; `randomBytes(n)`; `sign` and `verify` for ed25519, ecdsa-p256-sha256, ecdsa-sha256-p1363 (IEEE P1363 `r‖s` on the key's curve: P-256, P-384, P-521 or secp160r1) and rsa-pkcs1-sha256 (PEM or base64 DER keys); `publicKey(privateKey)` (base64 SPKI); AES-GCM and AES-CBC `encrypt`/`decrypt`. Encoding helpers: `base64`, `base64url`, `hex`, fixed-width `base58` (Bitcoin alphabet), `utf8`. |
| `secrets.get(name)`, `vars.get(name)` | Per-workflow, set by admins in the UI or with `autablectl workflow secret set`. Secret values are never returned by the API. |
| `tables(name)`, `users`, `files` | The same typed API as the app SDK, executed server-side as the `workflow:<id>` subject and limited by that subject's grants. |
| `log.info/warn/error(message, data?)` | Stored with the run. |

Standard globals: `TextEncoder`, `TextDecoder`, `URL`,
`URLSearchParams`, `atob`, `btoa`, `setTimeout`/`clearTimeout` (within
the run) and `console.*` (mapped to the run log). `Date` and
`Math.random` behave normally.

### Long-running processes

There is no waiting inside a run. A process that spans hours or days —
an approval, a multi-stage order — keeps its state in table rows and is
advanced by triggers:

- **External approvals** happen in the IM or ERP system. A
  `schedule`-triggered workflow polls the external system for the status
  of open requests (rows whose state says "pending") and writes the
  results into those rows.
- **Stages** are driven by `recordChanged`: each row change fires the
  workflow again, which looks at the row's state (`event.change`), does
  the next stage's work, and writes the next state. Use `fields` to react
  only to the state field. Own writes do not re-trigger the workflow by
  default; a second workflow (or a user) writing the row does.
- **Idempotence** comes from the rows: check the current state before
  acting, and use upsert or unique fields where duplicates would hurt.

`webhook` is a plain feature for systems that push events or need an
HTTP endpoint; a webhook run usually just records what it received in a
row.

### Runs

- **Status:** a run is `queued`, `running`, `succeeded`, `failed` or
  `cancelled`.
- **Stored:** the system database keeps each run's trigger, event (or
  input), pinned version, status, output, error, logs and timestamps,
  and who started it (manual runs).
- **Pinned version:** a run uses the version that was published when it
  was queued, even if a newer one is published before it starts.
- **Queue:** queued runs live in memory. On startup, runs left `queued`
  or `running` by a previous process are marked `failed` with
  `interrupted by server restart`.
- **Concurrency:** runs execute in parallel up to a worker limit (8).
  Runs of one workflow are not serialized; use table constraints and
  row state for ordering.
- **Cancel:** a queued run is cancelled before it starts; a running run
  is interrupted.
- **Limits:**
  - Each run has a time limit: the workflow's `timeout_seconds`
    (default 10 min), enforced with `Interrupt`.
  - Webhook runs must respond within 15 s; they run immediately, not
    through the queue, but share the worker limit.
  - Output is capped at 1 MiB, logs at 1000 entries per run.

### Runners

A runner becomes an **egress proxy**. It accepts `fetch` jobs (method,
URL, headers, body, timeout) from the server and returns the response.
It executes no code and holds no secrets beyond what each request
carries, so a new integration never requires updating a runner. Runner
tokens stay per database.

### Runtime

- **Engine:** goja inside the server binary. async/await and generators
  are native in the pinned version.
- **Event loop:** a small loop per run. Host operations such as `fetch`
  run in goroutines and settle their promises on the loop.
- **Bundles:** esbuild bundles pure-JS npm packages. Node built-ins are
  not available.

### Implementation decisions

- **Packages:** `internal/wfruntime` (goja runtime, event loop, globals,
  host API, crypto, trigger validation), `internal/cron` (5-field cron
  parser, no dependency), `internal/egress` (the HTTP client behind
  `fetch`, shared by the server and runners), `internal/wfsdk` (embedded
  SDK and version rule), `internal/sdkversion` (MAJOR.MINOR rule shared
  with the app SDK), `internal/appbundle` (bundle kinds: app, workflow
  and library); engine, host, library resolver and handlers live in
  `internal/api`.
- **Triggers are read on every publish** (also when re-publishing an old
  version for a rollback), because `recordChanged` declarations name
  tables and fields that may have changed; the result is stored with the
  version. An upload with `publish=1` whose triggers are invalid stores
  nothing.
- **Webhook runs** run immediately on the request (not through the queue)
  but take a worker slot; their limit is the smaller of 15 s and the
  workflow timeout. Unknown paths and disabled or unpublished workflows
  answer `404`. A thrown error answers `500 {error, run_id}`, the time
  limit `504`.
- **Schedules** are checked once a minute in process; each trigger fires
  at most once per minute. Missed minutes are not caught up.
- **recordChanged** creates one run per matching trigger; a workflow
  whose subject can read none of the table's fields is not fired.
- **Deleting a workflow** cancels its queued and running runs.
- **Run ids** are numbers. A run's `version` is the published version at
  queue time.

## Storage

The system database holds these tables:

| Table | Contents |
|---|---|
| `workflow_models` | id, database, name, title, published version, latest version, enabled, timeout seconds, history retention days, creator, timestamps |
| `workflow_version_models` | workflow, version, sdk, message, size, bundle (blob), triggers (JSON, stored when the version is published), libraries (JSON names of the imported libraries, stored at the same time), created by, timestamp |
| `workflow_secret_models` / `workflow_var_models` | workflow, name, value |
| `workflow_run_models` | id, workflow, version, trigger, status, event (JSON), output (JSON), error, started by, libraries (JSON `[{name, version}]` loaded by the run), created / started / finished timestamps |
| `workflow_log_models` | run, time, level, message, data |
| `library_models` | id, name (unique), title, published version, latest version, creator, timestamps |
| `library_version_models` | library, version, sdk, message, size, bundle (blob), imports (JSON, stored when published), created by, timestamp |

The library tables and the two `libraries_json` columns (which have a
default) are created by AutoMigrate on existing databases; no schema
migration is needed (`TestOpenAddsLibrariesToAVersion8Database`).

Retention applies per workflow (`history_retention_days`, 1–3650,
default 30): finished runs older than that are deleted daily, with their
logs.

Migration 7 → 8 does the following:

- drops the old workflow table; the old run history (LevelDB `whistory_`
  keys) is deleted at server startup, because LevelDB is not part of the
  system database transaction;
- deletes every grant and role membership held by `workflow:<id>`
  subjects (new ids would otherwise inherit old grants), and every
  `workflow` scope grant;
- the repository sync stops staging the `workflow/` directory. Scripts
  already written there are left untouched.

Deleting a workflow deletes its versions, secrets, vars, runs and logs,
the grants on it and the grants and role memberships of its subject.

## API

```
GET    /api/workflows/sdk                                   {version, min_major, files}
GET    /api/databases/{db}/workflows                        list (admins: all; users: runnable manual triggers)
GET    /api/databases/{db}/workflows/{name}                 detail: versions, triggers, secrets (names only), vars, settings
POST   /api/databases/{db}/workflows/{name}/versions        bundle upload (?message=&publish=1)
POST   /api/databases/{db}/workflows/{name}/publish         {version}
PATCH  /api/databases/{db}/workflows/{name}                 {enabled?, timeout_seconds?, history_retention_days?}
PUT    /api/databases/{db}/workflows/{name}/secrets/{key}   {value} ; DELETE to remove
PUT    /api/databases/{db}/workflows/{name}/vars/{key}      {value} ; DELETE to remove
GET    /api/databases/{db}/workflows/{name}/versions/{v}/source
DELETE /api/databases/{db}/workflows/{name}
POST   /api/databases/{db}/workflows/{name}/runs            {trigger?, input?, wait?} — manual trigger; wait: true waits up to 60 s
GET    /api/databases/{db}/workflows/{name}/runs            ?status=&limit=&before=
GET    /api/databases/{db}/workflows/{name}/runs/{id}       run with event and logs
POST   /api/databases/{db}/workflows/{name}/runs/{id}/cancel
ANY    /api/hooks/{db}/{name}/{path}                        webhook trigger, no session; the code authenticates the request

GET    /api/libs                                            libraries, each with imported_by
GET    /api/libs/{name}                                     detail: library (with imported_by), versions, sdk
POST   /api/libs/{name}/versions                            bundle upload (?message=&publish=1)
POST   /api/libs/{name}/publish                             {version} — also the rollback
GET    /api/libs/{name}/versions/{v}/source
GET    /api/libs/{name}/types                               {name, version, types}: dist/index.d.ts of the published version
DELETE /api/libs/{name}                                     409 while a published version imports it
```

Every `/api/libs` endpoint is admin-only.

Users with a run grant may use only the list (for runnable workflows),
`POST .../runs` with a manual trigger, and `GET .../runs/{id}` for the
runs they started, which show status and output only. A disabled or
unpublished workflow cannot be started by them (`409`); admins may start
the manual trigger of a disabled workflow to test it.

Response shapes:

- **Workflow:** `{id, database_name, name, title, enabled,
  published_version, latest_version, timeout_seconds,
  history_retention_days, creator_id, created_at, updated_at, triggers,
  last_run}`. `triggers` is `[{name, kind, ...declaration, url?}]` from
  the published version (`url` for webhooks). Non-admins get `{id,
  database_name, name, title, enabled, published_version, triggers}`
  with manual triggers only.
- **Detail:** `{workflow, versions: [{version, sdk, message, size,
  libraries, created_by, created_at, published, supported, sdk_error?}], secrets:
  [name], vars: {name: value}, sdk: {version, min_major}}`.
- **Run:** `{id, workflow_id, version, trigger, status, event, output,
  error, started_by, created_at, started_at, finished_at, logs: [{time,
  level, message, data}], libraries?: [{name, version}]}`; lists omit
  `event`, `logs` and `libraries`; non-admins get
  `{id, workflow_id, trigger, status, output, error, created_at,
  started_at, finished_at}`.
- `POST .../runs` answers `201` with the run, or with `wait: true` `200`
  when it finished within 60 s and `202` when it is still going.
- Upload answers like apps (`201` creating, `200` otherwise) with
  `{workflow, version}`; publish answers with the workflow.
- **Library:** `{id, name, title, published_version, latest_version,
  creator_id, created_at, updated_at, imported_by: [{kind: "workflow" |
  "library", database_name?, name, title, version}]}`; detail `{library,
  versions: [{version, sdk, message, size, imports, created_by,
  created_at, published, supported, sdk_error?}], sdk}`. Upload answers
  `{library, version}`, publish the detail.

## Command line

```
autablectl workflow init DIR --database DB --name NAME
autablectl workflow sdk [DIR]
autablectl workflow push [DIR] [-m MSG] [--publish] [--no-build]
autablectl workflow list | versions | publish | rollback | pull | delete
autablectl workflow run DB NAME [--trigger T] [--input JSON] [--wait]
autablectl workflow runs DB NAME [--status S] ; workflow run-log DB NAME RUN_ID
autablectl workflow secret set|unset DB NAME KEY ; workflow var set|unset DB NAME KEY VALUE
autablectl workflow enable|disable DB NAME
autablectl lib init DIR --name NAME ; lib sdk [DIR] ; lib push [DIR] [-m MSG] [--publish] [--no-build]
autablectl lib list | versions NAME | publish NAME VERSION | rollback NAME | pull NAME DIR | delete NAME
```

## Web UI

Workflows get the same shape as apps:

- **List page:** each workflow's status, published version, triggers and
  last run.
- **Detail page:**
  - versions with publish and roll back;
  - triggers, including webhook URLs;
  - settings;
  - secrets, which can be set but not read, and vars;
  - the runs list, with run detail: event, logs, output and errors;
  - starting a manual run with JSON input.

An admin **Libraries** page in the Manage section lists libraries with
their published version and importers; a library's detail shows its
versions (publish, roll back, source download), its imports and the
workflows and libraries that import it. A run's detail shows the library
versions it loaded.

There is no in-browser code editor; code lives in projects. Ordinary
users see the workflows they may run, can start the manual trigger with a
form built from its declared `fields`, and see their own runs' status.
