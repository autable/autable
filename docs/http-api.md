# HTTP API

Everything in autable goes through one HTTP API: the web UI, hosted apps
(through the app SDK), and `autablectl`. This page lists every route and
says how requests are authenticated and authorized. For row bodies (queries,
operators, sorts, views, paging, upsert, file fields, history) see
[query.md](query.md); for the permission model see
[permissions.md](permissions.md).

Use `autablectl api METHOD PATH [--data JSON]` to call any route with your
stored login. It sends `Authorization: Bearer <token>` and, when `--data` is
given (inline JSON, `@file`, or `-` for stdin), `Content-Type:
application/json`. It can only send JSON bodies: upload files with
`autablectl file upload` and bundles with `autablectl app|workflow|lib push`.

## Authentication

A request is authenticated by a session token, sent one of two ways. When
both are present the bearer token wins.

| Credential | How it is sent | How it is obtained | Lifetime |
|---|---|---|---|
| Bearer token | `Authorization: Bearer <token>` | `autablectl login` (below), or any session token | 90 days for CLI logins |
| Session cookie | `autable_session`, or `__Host-autable_session` when `server.public_url` is https (HttpOnly, SameSite=Lax, Path=/) | browser login: password or OIDC | 14 days |

Both are ordinary sessions of one user and carry exactly that user's
permissions. `POST /api/auth/logout` deletes the session it was sent with,
which also revokes a bearer token. Every authenticated route accepts both;
only the CLI authorization page and its form post need the browser cookie
(a bearer token cannot approve another token).

**CLI login flow** (`autablectl login --server URL`):

1. The CLI listens on a loopback port (1024-65535), creates a random
   `state` and a PKCE verifier, and opens
   `GET /api/auth/cli/authorize?port=P&state=S&challenge=C`, where `C` is
   base64url (no padding) of SHA-256 of the verifier.
2. A browser without a session is sent to `/login?redirect=...` first. The
   signed-in user confirms on the page, which posts the form back to
   `POST /api/auth/cli/authorize`.
3. The server redirects (`303`) to
   `http://127.0.0.1:P/callback?code=...&state=S`. The code is single-use
   and expires after 5 minutes.
4. The CLI posts `{"code", "verifier"}` to `POST /api/auth/cli/token` and
   gets `{"token", "expires_at", "user"}` (`expires_at` in milliseconds).

The CLI stores the token in `<user config dir>/autable/autablectl.json`;
`AUTABLE_SERVER` and `AUTABLE_TOKEN` override it for one invocation.
`autablectl login --token TOKEN` stores an existing token after checking it
with `GET /api/auth/me`. `autablectl app dev` runs the app's dev server with
a proxy that adds the bearer token to every `/api` request.

An invalid or expired token is a `401` on every route that needs a user;
`GET /api/metadata` without any credential answers `200` with
`{"databases": []}`.

### Cross-site request checks

Applied to every `/api/...` request with method POST, PUT, PATCH, or DELETE,
**except** requests carrying a non-empty `Authorization: Bearer` header and
workflow webhooks (`/api/hooks/...`):

1. If `Sec-Fetch-Site` is present it must be `same-origin` or `none`;
   anything else is `403 cross-site request rejected`.
2. Otherwise, if `Origin` is present, its scheme and host must equal those
   of `server.public_url`, or its host (with port) must equal the request's
   `Host` header; otherwise `403`. `Origin: null` fails.
3. A request with neither header passes these checks.
4. Endpoints that read a JSON body require `Content-Type: application/json`
   (parameters such as `charset` allowed) and answer `415` otherwise. Bearer
   requests are exempt; multipart and bundle uploads do not read JSON.

`POST /api/auth/cli/authorize` additionally rejects any `Sec-Fetch-Site`
other than `same-origin`.

### App SDK version header

The app SDK sends `X-Autable-SDK: MAJOR.MINOR` (its `SDK_VERSION`) on every
call. When a request to `/api/...` carries this header, the server checks it
before anything else: a version newer than the server's SDK, older than the
oldest major it still serves, or malformed is rejected with `410 Gone` and
an `{"error"}` naming the server's range and the fix (`autablectl app sdk`
and rebuild). Requests without the header are not checked. The server's
current version and oldest major come from `GET /api/apps/sdk`
(`{version, min_major}`); a minor release only adds API, a major one may
break it. Bundles whose manifest SDK is unsupported are refused at upload
and publish (`422`), and their pages answer `410`.

### Who may call what

- **public**: no session needed.
- **user**: any signed-in user.
- **grant**: any signed-in user; the data or resource is checked against
  their grants (fields, views, records, files, app open, workflow run).
- **admin**: only users whose email is listed in the server's `auth.admins`
  config (case-insensitive). Admins bypass every data check. Everyone else
  gets `403` with `permission denied: this action requires an admin`.
  `GET /api/auth/me` reports `is_admin`. Workflow and role subjects are
  never admins.

### Errors

Handlers answer errors as JSON `{"error": "message"}`. Unknown routes and
wrong methods get the router's plain-text `404` / `405`, unknown
`{resource}` segments a plain-text `404`. JSON bodies are decoded strictly:
an unknown key is `400` (`json: unknown field "..."`).

| Status | Typical cause |
|---|---|
| 400 | invalid input, failed validation, unknown table or field |
| 401 | not signed in, invalid or expired token, wrong password |
| 403 | admin required, missing grant, cross-site request |
| 404 | no such database, app, workflow, run, or file; a row outside the caller's row set; a run someone else started |
| 409 | email already registered, workflow disabled or unpublished, run already finished, library still imported, concurrent first push |
| 410 | unsupported app SDK version |
| 413 | file above the upload limit (default 20 MiB), bundle above 30 MiB, webhook body above 5 MiB |
| 415 | JSON body without `Content-Type: application/json` (cookie requests) |
| 422 | bundle rejected: unsupported SDK, compile error, invalid triggers or library imports |
| 502 / 503 / 504 | file store or identity provider failure / file storage not configured / webhook run did not answer in time |

## Routes

Path segments in braces are names (`{db}`, `{table}`, `{name}`) or numeric
ids. Percent-encode segments containing spaces, `/`, `?`, or `#`. "Used by"
names the app SDK function (`SDK`) and the `autablectl` command (`CLI`);
the web UI uses nearly everything.

### Auth, session, users

| Method | Path | Who | Purpose | Used by |
|---|---|---|---|---|
| GET | `/api/auth/config` | public | `{password_enabled, oidc_enabled, oidc_providers: [{name, issuer_url, scopes}]}` | |
| POST | `/api/auth/register` | public | `{email, display_name, password}`; creates the user, sets the cookie, `201` with the user; `409` if the email exists; `404` when password auth is off | |
| POST | `/api/auth/login` | public | `{email, password}`; sets the cookie, returns the user | |
| GET | `/api/auth/oidc/{provider}/start` | public | redirects to the provider; `?redirect=/local/path` is where to land afterwards | |
| GET | `/api/auth/oidc/{provider}/callback` | public | provider callback; sets the cookie and redirects | |
| GET | `/api/auth/me` | user | `{id, email, display_name, provider, is_admin}` | SDK `currentUser`; CLI `whoami`, `login --token` |
| POST | `/api/auth/logout` | public | deletes the presented session, clears the cookie; `{"ok": true}` | CLI `logout` |
| GET | `/api/auth/cli/authorize` | cookie | HTML confirmation page; `?port&state&challenge` | CLI `login` (browser) |
| POST | `/api/auth/cli/authorize` | cookie | form post from that page; redirects to the CLI's loopback port with a code | browser |
| POST | `/api/auth/cli/token` | public | `{code, verifier}` → `{token, expires_at, user}` | CLI `login` |
| GET | `/api/users` | user | users matching `?query=` (name or email), at most 20: `[{id, email, display_name, provider}]` | SDK `searchUsers` |

### Metadata: databases, tables, fields, views

| Method | Path | Who | Purpose | Used by |
|---|---|---|---|---|
| GET | `/api/metadata` | grant | the catalog the caller can see: `{databases: [{name, tables: [{name, display_name, fields, views, permission_level}]}]}`. Non-admins see readable fields (with their bit level) and readable views; admins see all, plus the built-in `all` view | CLI `meta`, `app sdk`, `workflow sdk` (schema types) |
| POST | `/api/databases` | admin | `{name}`; creates a database, `201` | |
| POST | `/api/databases/{db}/tables` | admin | creates a table from `{name, display_name?, fields?, views?}`, `201` | |
| PUT | `/api/databases/{db}/tables/{table}` | admin | merges table metadata: `display_name` if non-empty; `fields` and `views` merged by name (listed ones added or replaced, omitted ones untouched; a field with `"deleted": true` is soft-deleted). The name cannot change; the built-in `all` view is ignored | |
| PATCH | `/api/databases/{db}/tables/{table}/fields/{field}/position` | admin | exactly one of `{"position": "start"}`, `{"before": F}`, `{"after": F}` | |
| POST | `/api/tables/{db}/{table}/fields` | admin; others only when nothing changes | "ensure fields": `{"fields": ["a", {"name": "b", "type": "int"}]}` or `{"fields": {"a": "string"}}`; types `string`, `int`, `float` only. Creates missing fields and restores deleted ones; answers `{created, restored, existing, fields}`. A non-admin with any grant on the table succeeds only if every field already exists with that type, otherwise `403` | CLI `fields add` |

Field types and view definitions are described in [query.md](query.md).

### Rows and history

All are **grant**: field bits, the view row set, and `record` create/delete
grants apply as described in [query.md](query.md) and
[permissions.md](permissions.md).

| Method | Path | Purpose | Used by |
|---|---|---|---|
| GET | `/api/tables/{db}/{table}/rows` | rows of `?view=` (default `all`), optional `?sort_field=&sort_direction=asc\|desc` | |
| POST | `/api/tables/{db}/{table}/rows/query` | `{view?, query?, sorts?, search?, limit?, offset?}` → `[{record_id, values}]` | SDK `table().list/query/get`; CLI `rows get` |
| POST | `/api/tables/{db}/{table}/rows/page` | same body → `{rows, total}` | SDK `page`; CLI `rows list` |
| POST | `/api/tables/{db}/{table}/rows` | `{values}` → `201` row | SDK `create`; CLI `rows create` |
| PATCH | `/api/tables/{db}/{table}/rows/{id}` | `{values}` (changed fields only) → row | SDK `update`; CLI `rows update` |
| POST | `/api/tables/{db}/{table}/rows/upsert` | `{match_field, values}` → `{operation: create\|update\|noop, record_id, values}` | SDK `upsert`; CLI `rows upsert` |
| DELETE | `/api/tables/{db}/{table}/rows/{id}` | deletes; returns the last values | SDK `delete`; CLI `rows delete` |
| GET | `/api/tables/{db}/{table}/rows/{id}/history` | changes oldest first, redacted to readable fields | SDK `history`; CLI `rows history` |

### Files

All are **grant** (a `file` grant on the table; download and metadata also
need the bound row in the caller's row set). Details in
[query.md](query.md#files).

| Method | Path | Purpose | Used by |
|---|---|---|---|
| POST | `/api/files` | `multipart/form-data`: `file`, `database_name`, `table_name` (both required), `record_id?` → `201` file record | SDK `files.upload`; CLI `file upload` |
| POST | `/api/files/metadata` | `{ids: [...]}` → records of the visible files only | SDK `files.metadata` |
| GET | `/api/files/{id}` | file bytes; images and PDFs inline, others as attachment. Without a session, `Accept: text/html` redirects to the login page, otherwise `401` | SDK `files.url`; CLI `file download` |

### Permissions and roles

All are **admin**. Subjects are a user id (from `/api/users`), a role
`role:<db>:<name>`, or a workflow `workflow:<id>`.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/permissions/grants` | saves one grant `{subject_id, scope, resource, field, level}`, `201`; level `0` revokes. Saving a `field`/`field_set`, `view`/`view_set`, or `app`/`app_set` grant deletes the subject's grants of the other kind on the same table (for `app`/`app_set`, the same database) |
| GET | `/api/databases/{db}/roles` | `[{id, database_name, name, subject_id, grants, members, member_users, member_workflows, created_at, updated_at}]` |
| POST | `/api/databases/{db}/roles` | creates a role `{name}`, `201` (set grants and members with the PUTs below, which validate them) |
| PUT | `/api/databases/{db}/roles/{role}/grants` | `{grants: [...]}` replaces the role's grants; each must belong to `{db}`; field/field_set (and view/view_set, app/app_set) on the same resource are mutually exclusive |
| PUT | `/api/databases/{db}/roles/{role}/members` | `{members: [{type: "user"\|"workflow", id}]}` replaces the members; workflows must belong to `{db}` |

Grant scopes, resources, and levels:

| Scope | `resource` | `field` | `level` |
|---|---|---|---|
| `field_set` | `db.table` | empty | bitmask 1 read, 2 update, 4 create (7 = all) |
| `field` | `db.table` | field name | same bitmask |
| `view_set` | `db.table` | empty | 1 (read) |
| `view` | `db.table` | view name (`all` for the built-in view) | 1 |
| `record` | `db.table` | `create` or `delete` | 2 |
| `file` | `db.table` | empty | 1 or 2 (1 is enough to upload and download) |
| `app_set` | database name | empty | 1 (open every app of the database) |
| `app` | app id | empty | 1 (open) |
| `workflow` | workflow id | empty | 1 (run the manual triggers) |

### Apps

| Method | Path | Who | Purpose | Used by |
|---|---|---|---|---|
| GET | `/api/apps/sdk` | user | `{version, min_major, files: {name: source}}` | CLI `app init`, `app sdk` |
| GET | `/api/databases/{db}/apps` | grant | apps the caller may open: `[{id, database_name, name, title, published_version, latest_version, permission_level, url, ...}]` | CLI `app list` |
| GET | `/api/databases/{db}/apps/{name}` | grant (open) | `{app, versions, sdk}`; `versions` (`{version, sdk, message, size, created_by, created_at, supported, sdk_error?, preview_url}`) is empty for non-admins | CLI `app versions`, `app rollback`, `app pull` |
| POST | `/api/databases/{db}/apps/{name}/versions` | admin | body: the gzipped bundle built by `app push`; `?message=&publish=1`. The first push creates the app (`201`, else `200`); the manifest's database and name must match the URL. Answers `{app, version}` | CLI `app push` |
| POST | `/api/databases/{db}/apps/{name}/publish` | admin | `{version}`; also how rollback works. Answers the app | CLI `app publish`, `app rollback` |
| GET | `/api/databases/{db}/apps/{name}/versions/{v}/source` | admin | source as `.tar.gz` | CLI `app pull` |
| DELETE | `/api/databases/{db}/apps/{name}` | admin | deletes the app and its versions, `204` | |
| GET | `/apps/{db}/{name}` | public | `301` to the same path with a trailing slash (query kept) | |
| GET | `/apps/{db}/{name}/{path...}` | grant (open) | serves the published version. Unknown paths fall back to `index.html` unless the last segment has a dot (`404`); `index.html` gets a `<base href>` of the app root | browser |
| GET | `/apps/{db}/{name}/@{v}/{path...}` | admin | preview of any version | `app push` prints it |

Hosting pages redirect to `/login?redirect=...` without a session and answer
errors as small HTML pages: `403` no access, `404` unknown app or version or
nothing published, `410` the version's SDK is no longer supported.

### Workflows

Workflow code does not call this API: it runs inside the server and reaches
tables, files, users, secrets, logs, and outbound `fetch` through host
functions wrapped by the workflow SDK, as the `workflow:<id>` subject with
that subject's grants. These routes are for managing and starting
workflows.

| Method | Path | Who | Purpose | Used by |
|---|---|---|---|---|
| GET | `/api/workflows/sdk` | user | `{version, min_major, files}` | CLI `workflow init/sdk`, `lib init/sdk` |
| GET | `/api/databases/{db}/workflows` | grant | admins: every workflow with settings, published `triggers` (webhooks with `url`), `last_run`. Others: enabled, published workflows they may run that have a manual trigger, as `{id, database_name, name, title, enabled, published_version, permission_level, triggers}` (manual only) | CLI `workflow list` |
| GET | `/api/databases/{db}/workflows/{name}` | admin | `{workflow, versions, secrets (names only), vars, sdk}` | CLI `workflow versions`, `rollback`, `pull` |
| PATCH | `/api/databases/{db}/workflows/{name}` | admin | settings `{enabled?, timeout_seconds? (1-86400), history_retention_days? (1-3650)}` | CLI `workflow enable/disable` |
| DELETE | `/api/databases/{db}/workflows/{name}` | admin | cancels its runs, deletes it with versions, settings, runs; `204` | CLI `workflow delete` |
| POST | `/api/databases/{db}/workflows/{name}/versions` | admin | gzipped bundle; `?message=&publish=1`. With `publish`, triggers and `@lib/` imports are validated first (`422`). First push creates it (`201`). Answers `{workflow, version}` | CLI `workflow push` |
| POST | `/api/databases/{db}/workflows/{name}/publish` | admin | `{version}`; re-reads the triggers against the current schema (`422` if invalid); also the rollback | CLI `workflow publish`, `rollback` |
| GET | `/api/databases/{db}/workflows/{name}/versions/{v}/source` | admin | source as `.tar.gz` | CLI `workflow pull` |
| PUT | `/api/databases/{db}/workflows/{name}/secrets/{key}` | admin | `{value}`, `204`. Values are never returned | CLI `workflow secret set` |
| DELETE | `/api/databases/{db}/workflows/{name}/secrets/{key}` | admin | `204`, `404` if absent | CLI `workflow secret unset` |
| PUT | `/api/databases/{db}/workflows/{name}/vars/{key}` | admin | `{value}`, `204` | CLI `workflow var set` |
| DELETE | `/api/databases/{db}/workflows/{name}/vars/{key}` | admin | `204`, `404` if absent | CLI `workflow var unset` |
| POST | `/api/databases/{db}/workflows/{name}/runs` | grant (run) | starts a manual trigger: `{trigger?, input?, wait?}` (body at most 1 MiB; `trigger` may be omitted when there is exactly one manual trigger; `input` must be an object; values given for the trigger's declared input fields must have the declared type). `201` with the run; with `wait: true`, `200` if it finished within 60 s, else `202`. `409` when unpublished, or disabled for non-admins | SDK `runWorkflow`; CLI `workflow run` |
| GET | `/api/databases/{db}/workflows/{name}/runs` | admin | runs newest first, without event and logs; `?status=queued\|running\|succeeded\|failed\|cancelled&limit=` (default 50, max 500) `&before=<run id>` | CLI `workflow runs` |
| GET | `/api/databases/{db}/workflows/{name}/runs/{id}` | admin, or the user who started it | admins: the full run with `event`, `logs`, `libraries`; the starter: `{id, workflow_id, trigger, status, output, error, created_at, started_at, finished_at}`; anyone else `404` | SDK `workflowRun`; CLI `workflow run-log`, `run --wait` |
| POST | `/api/databases/{db}/workflows/{name}/runs/{id}/cancel` | admin | cancels a queued or running run (`409` otherwise); answers the run | |
| any | `/api/hooks/{db}/{name}/{path...}` | public | webhook trigger whose declared path equals `{path}`. No session and no cross-site checks: the workflow code authenticates the request, and its return value becomes the response. Body at most 5 MiB; the run must answer within 15 s, or the workflow's timeout if shorter (`504 {error, run_id}`). `404` when the workflow is disabled, unpublished, or has no such path | external callers |

Run `status` is `queued`, `running`, `succeeded`, `failed`, or
`cancelled`; times are milliseconds since the Unix epoch. Secret and var
names must match `^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`.

### Libraries

Shared modules workflows import as `@lib/<name>`; a run always loads the
published version. All routes are **admin**.

| Method | Path | Purpose | Used by |
|---|---|---|---|
| GET | `/api/libs` | libraries, each with `imported_by` | CLI `lib list`, `workflow sdk`, `lib sdk` |
| GET | `/api/libs/{name}` | `{library, versions, sdk}` | CLI `lib versions`, `rollback`, `pull` |
| POST | `/api/libs/{name}/versions` | gzipped bundle; `?message=&publish=1` (with `publish`, imports and cycles are checked first, `422`). Answers `{library, version}` | CLI `lib push` |
| POST | `/api/libs/{name}/publish` | `{version}`; also the rollback. Answers the detail | CLI `lib publish`, `rollback` |
| GET | `/api/libs/{name}/versions/{v}/source` | source as `.tar.gz` | CLI `lib pull` |
| GET | `/api/libs/{name}/types` | `{name, version, types}`: type declarations of the published version | CLI `workflow sdk`, `lib sdk` |
| DELETE | `/api/libs/{name}` | `204`; `409` while a published workflow or library version imports it | CLI `lib delete` |

### Runners

Runners are per-database egress proxies for workflow `fetch(url, {via})`.

| Method | Path | Who | Purpose |
|---|---|---|---|
| GET | `/api/databases/{db}/runners` | admin | `{token: {exists, created_at?}, runners: [{name, version, connected_at}]}` |
| POST | `/api/databases/{db}/runners` | admin | rotates the database's runner token and disconnects its runners; `{token, created_at}` (the only time the token is shown) |
| GET | `/api/runner/ws` | runner token | WebSocket for the `autable-runner` process; `Authorization: Bearer <runner token>` (a runner token, not a session token). Answers plain-text `401` without a valid one |

### Repository mirror

There are no mirror routes. When the server is configured with a git
repository, metadata changes (database, table, field moves, table updates)
and every app, workflow, and library push, publish, and delete are written
there as a backup. Nothing is read back from it: change the live server
through this API.

## Examples

```sh
autablectl api GET /api/auth/me
autablectl api GET /api/metadata
autablectl api POST /api/tables/crm/contacts/rows/page \
  --data '{"view":"all","sorts":[{"field":"ct_record_id","direction":"desc"}],"limit":20}'
autablectl api PATCH /api/tables/crm/contacts/rows/42 --data '{"values":{"email":"ada@example.com"}}'
autablectl api POST /api/files/metadata --data '{"ids":[15,16]}'

# admin: grants and roles
autablectl api GET '/api/users?query=ada'
autablectl api POST /api/databases/crm/roles --data '{"name":"support"}'
autablectl api PUT /api/databases/crm/roles/support/grants --data '{"grants":[
  {"subject_id":"role:crm:support","scope":"view","resource":"crm.contacts","field":"all","level":1},
  {"subject_id":"role:crm:support","scope":"field_set","resource":"crm.contacts","field":"","level":7}]}'
autablectl api PUT /api/databases/crm/roles/support/members --data '{"members":[{"type":"user","id":"USER_ID"}]}'

# admin: workflow runs
autablectl api GET '/api/databases/crm/workflows/sync_orders/runs?status=failed&limit=10'
autablectl api POST /api/databases/crm/workflows/sync_orders/runs/17/cancel
autablectl api PATCH /api/databases/crm/workflows/sync_orders --data '{"timeout_seconds":120}'

# start a manual trigger and wait up to 60 s
autablectl api POST /api/databases/crm/workflows/sync_orders/runs --data '{"input":{"since":"2026-01-01"},"wait":true}'
```

Responses print as JSON; an empty `204` prints `{"ok": true}`. A non-2xx
response makes the command exit `1` with the status and the server's
`error` message.
