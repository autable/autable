# Workflows v2: code projects on platform primitives

Status: accepted; replaces the node-based workflows. No compatibility:
existing workflows are rewritten on the new model.

## Why

The old model was one JavaScript file with `instances`, `trigger` and
`run`, evaluated synchronously in goja. The only way to reach the outside
world was a **node**: Go code compiled into autable. That had four costs:

- **Every integration was a release.** Even one HTTP call or signature
  needed a new node, a new build and an upgrade.
- **Triggers were hard-coded** in the API server. The webhook body format
  was fixed, so third-party callbacks could not be received.
- **Nothing was durable.** There were no retries, no waiting, no stored
  queue, and a run was written only at the end. Approvals were polled.
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
deployment. Adding or fixing one is a workflow push, not an autable
release. There are no nodes.

### Project layout

```
autable.workflow.json   {"database","name","title","sdk"}
src/index.ts            exports `triggers` and `run`
src/autable/            vendored SDK + schema.ts (autablectl workflow sdk)
dist/workflow.js        esbuild output (IIFE assigning globalThis.__workflow)
```

The bundle is the same gzip tar format as apps: manifest, `dist/`,
`source/`. `dist/workflow.js` is required.

### Code shape

```ts
import { schedule, recordChanged, webhook, manual, step, waitFor, signal, fetch, secrets, tables, log } from "./autable";

export const triggers = {
  daily: schedule({ cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" }),
  changed: recordChanged({ table: "订单", operations: ["create", "update"] }),
  callback: webhook({ path: "approval" }),
  start: manual({ fields: [{ name: "orderId", label: "Order", type: "number" }] }),
};

export async function run(event: TriggerEvent<typeof triggers>) {
  switch (event.trigger) {
    case "callback": {
      // event.request: { method, path, query, headers, body (bytes), text(), json() }
      const payload = verifyAndDecode(event.request, secrets.get("callback_key"));
      await signal(`approval:${payload.instanceId}`, payload);
      return { status: 200, body: "success" };   // webhook runs answer the HTTP request
    }
    case "start": {
      const created = await step("create approval", () => createApproval(event.input.orderId));
      const result = await waitFor<ApprovalResult>(`approval:${created.id}`, { timeout: "7d" });
      await step("write result", () => tables("订单").update(event.input.orderId, { 状态: result.status }));
    }
  }
}
```

- **Triggers** are read once, when a version is published: the bundle is
  evaluated and the `triggers` export is stored. Publishing fails when the
  declarations are invalid.
- **Trigger kinds** are `schedule` (cron with seconds omitted, plus a
  timezone), `recordChanged` (table and operations; the event carries the
  row change filtered by the workflow subject's grants), `webhook` (path
  segment; the run gets the raw request) and `manual` (optional `fields` list of `{name,
  label, type: "string" | "number" | "boolean"}` used for the input form;
  started from the API, the CLI or an app).
- **`run`** receives `{ trigger, runId, ... }` with the trigger-specific
  fields: `scheduledAt`, `change`, `request` or `input`.

### Primitives (SDK 1.0)

| Primitive | Behaviour |
|---|---|
| `fetch(url, init?)` | `method`, `headers`, `body` (string, bytes or a JSON-able value with `json:`), `timeoutMs` (default 30 s), `via` (runner name). Returns `{ status, headers, text(), json(), bytes() }`. Responses are capped at 20 MiB. With `via`, the request is performed by that runner. |
| `crypto` | `hash(alg, data)` for sha1, sha256, sha512 and md5; `hmac(alg, key, data)`; `randomBytes(n)`; `sign` and `verify` for ed25519, ecdsa-p256-sha256 and rsa-pkcs1-sha256 (PEM keys); AES-GCM and AES-CBC `encrypt`/`decrypt`. Encoding helpers: `base64`, `base64url`, `hex`, `utf8`. |
| `secrets.get(name)`, `vars.get(name)` | Per-workflow, set by admins in the UI or with `autablectl workflow secret set`. Secret values are never returned by the API. |
| `tables(name)`, `users`, `files` | The same typed API as the app SDK, executed server-side as the `workflow:<id>` subject and limited by that subject's grants. |
| `log.info/warn/error(message, data?)` | Stored with the run. |
| `step(name, fn, { retries?, backoffMs? })` | Runs `fn` once per run and journals its JSON result. On failure it retries (default 0), then fails the run. |
| `sleep(duration)` | Durable timer; suspends the run. |
| `waitFor(key, { timeout? })` | Suspends until `signal(key, payload)`; resolves with the payload. On timeout it throws `TimeoutError`. |
| `signal(key, payload)` | Wakes every waiting run of this database's workflows that waits on `key`. |
| `now()`, `random()` | Journaled, so replay is deterministic. |

Durations are strings like `30s`, `15m`, `7d`, or milliseconds.

### Durable execution

- **Run status:** a run is `queued`, `running`, `waiting`, `succeeded` or
  `failed`.
- **Stored state:** runs, their journal (step results, `now`/`random`
  values, the signal and timer outcomes) and their logs live in the system
  database.
- **Suspending:** when the code awaits `sleep` or `waitFor`, the run is
  stored as `waiting`, with a wake time or a wait key, and its goja
  runtime is discarded.
- **Resuming:** a signal, the timer or a timeout queues the run again.
  Resuming evaluates the code from the top in a fresh runtime. Every
  journaled `step`, `now`, `random`, `sleep` and `waitFor` returns its
  recorded outcome instead of running again. Code between steps must
  therefore be deterministic; side effects belong inside `step`.
- **Pinned version:** a run always uses the version it started with, even
  after a newer one is published.
- **Restarts:** the queue is stored, so a restart resumes queued and
  running runs. A run that was mid-step re-executes that step, so steps
  should be idempotent where it matters.
- **Limits:**
  - Each execution slice (from start or resume until it suspends or
    finishes) has a CPU-time limit: the workflow's `timeout`, default
    10 min, enforced with `Interrupt`.
  - A run lasts at most 30 days.
  - The journal is capped at 10 MiB.
  - Webhook runs must respond within 15 s. They may call `signal` and
    `step`, but not suspend.
- **Concurrency:** runs of different workflows execute in parallel up to
  a worker limit. Runs of one workflow are not serialized; use `step` and
  table constraints for ordering.

### Runners

A runner becomes an **egress proxy**. It accepts `fetch` jobs (method,
URL, headers, body, timeout) from the server and returns the response.
It executes no code and holds no secrets beyond what each request
carries, so a new integration never requires updating a runner. Runner
tokens stay per database.

### Runtime

- **Engine:** goja inside the server binary. async/await and generators
  are native in the pinned version.
- **Event loop:** a small loop per execution slice. Host operations such
  as `fetch` run in goroutines and settle their promises on the loop.
- **Bundles:** esbuild bundles pure-JS npm packages. Node built-ins are
  not available.
- **Globals:** `TextEncoder`, `TextDecoder`, `URL`, `URLSearchParams`,
  `atob`, `btoa` and `setTimeout` (non-durable, short delays only) are
  provided; `console.log` maps to `log.info`.

## Storage

The system database holds these tables:

| Table | Contents |
|---|---|
| `workflow_models` | id, database, name, title, published version, latest version, enabled, creator, timestamps |
| `workflow_version_models` | workflow, version, sdk, message, bundle (blob), triggers (JSON), created by, timestamp |
| `workflow_secret_models` / `workflow_var_models` | workflow, name, value |
| `workflow_run_models` | id, workflow, version, trigger, status, event (JSON), journal (JSON), output, error, wait key, wake at, attempts, timestamps |
| `workflow_log_models` | run, time, level, message, data |

Retention applies per workflow (`history_retention_days`, default 30).
Waiting runs are never pruned.

Migration 7 → 8 does the following:

- drops the old workflow table and its LevelDB run history keys;
- deletes every grant and role membership held by `workflow:<id>`
  subjects (new ids would otherwise inherit old grants), and every
  `workflow` scope grant;
- removes the `workflow/` directory from repository sync.

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
POST   /api/databases/{db}/workflows/{name}/runs            {trigger, input} — manual trigger; {wait: true} waits up to 60 s for completion
GET    /api/databases/{db}/workflows/{name}/runs            ?status=&limit=&before=
GET    /api/databases/{db}/workflows/{name}/runs/{id}       run with journal and logs
POST   /api/databases/{db}/workflows/{name}/runs/{id}/cancel
ANY    /api/hooks/{db}/{name}/{path}                        webhook trigger, no session; the code authenticates the request
```

Users with a run grant may use only the list (for runnable workflows),
`POST .../runs` with a manual trigger, and `GET .../runs/{id}` for the
runs they started, which show status and output only.

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
  - the runs list, with run detail: the step journal, logs, waits and
    errors;
  - starting a manual run with JSON input.

There is no in-browser code editor; code lives in projects. Ordinary
users see the workflows they may run, can start the manual trigger with a
form built from its declared `fields`, and see their own runs' status.
