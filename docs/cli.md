# autablectl

`autablectl` is the command-line client for a running autable server. It talks
to the same HTTP API as the web UI, with the permissions of the user who signed
it in, so scripts and coding agents can read and change live tables and, with
an admin account, deploy workflows and hosted apps, instead of editing a
repository checkout
(the git repository autable pushes to is a backup; edits made there are never
pulled back).

Every command prints JSON on stdout and diagnostics on stderr. Exit codes:
`0` success, `1` the request failed, `2` the command line was invalid.

Release archives named `autable-<version>-cli-<os>-<arch>` contain the binary
for Linux (amd64, arm64), Windows (amd64), and macOS (arm64). To build it
yourself: `CGO_ENABLED=0 go build ./cmd/autablectl`.

## Signing in

```sh
autablectl login --server https://autable.example.com
```

`login` opens the browser at `/api/auth/cli/authorize`. If the browser is not
signed in it goes through the normal login page (password or OIDC) first; the
user then confirms on a page naming the account. The server redirects to a
loopback port the CLI listens on with a one-time code, and the CLI exchanges
the code, together with a PKCE verifier only it knows, for a session token.

- The token is an ordinary session lasting 90 days, stored in
  `<user config dir>/autable/autablectl.json` (mode 0600). It carries exactly
  the user's permissions.
- `autablectl logout` revokes it on the server and deletes the file.
- `--no-browser` prints the URL instead of opening it; the browser must run on
  the same computer, since the redirect goes to `127.0.0.1`.
- `--token TOKEN` stores an existing session token instead.
- `AUTABLE_SERVER` and `AUTABLE_TOKEN` override the stored login for a single
  invocation.

Any session token works as `Authorization: Bearer <token>` against the API, so
other HTTP clients can use the same token.

## Permissions

The CLI can do exactly what the signed-in user can do; the server checks every
request (see [permissions.md](permissions.md)). autable is admin-gated: code and
structure are written only by admins, the email addresses listed under
`auth.admins` in the server config.

- Any user, within their grants: read metadata, read and write rows, read row
  history, upload and download files, list the workflows they may run and
  start their manual trigger, list and open the apps they may open.
- Admins only: schema changes (`fields add` succeeds for anyone else only when
  every listed field already exists with the same type), everything else about
  workflows (push, publish, rollback, pull, delete, settings, secrets, vars,
  run history), and pushing, publishing, rolling back, or pulling apps.
- A non-admin's `workflow list` shows only the workflows they may run, with
  their manual triggers; `workflow run` works for those, and `workflow
  run-log` only for runs they started, showing status, output, and error.

`autablectl whoami` prints the signed-in user, including whether it is an
admin. When a non-admin runs an admin-only command, the CLI prints the server's
`403` message and exits `1`.

## Commands

Run `autablectl help` for the full list and `autablectl <command> -h` for a
command's flags. JSON-valued flags (`--values`, `--query`, `--input`,
`--fields`, `--data`) accept inline JSON, `@path` to read a file, or `-` to
read stdin.

### Structure

```sh
autablectl meta                      # every readable database, table, field, view
autablectl meta sales orders         # one table
autablectl fields add sales orders --fields '[{"name":"数量","type":"int"}]'
```

### Rows

```sh
autablectl rows list sales orders --query '{"field":"状态","operator":"=","value":"open"}' --limit 20
autablectl rows list sales orders --query '{"combinator":"or","rules":[...]}' --sorts '[{"field":"ct_record_id","direction":"desc"}]'
autablectl rows get sales orders 42
autablectl rows create sales orders --values '{"名称":"Widget","数量":3}'
autablectl rows update sales orders 42 --values '{"数量":4}'
autablectl rows upsert sales orders --match 名称 --values '{"名称":"Widget","数量":5}'
autablectl rows delete sales orders 42
autablectl rows history sales orders 42
```

`rows list` prints `{"rows": [...], "total": N}`. A `--query` with a single
`field` is wrapped into an `and` group.

### Workflows

A workflow is a TypeScript project, built and versioned like an app: it
exports `triggers` (`schedule`, `recordChanged`, `webhook`, `manual`) and
`run(event)`, uses the platform primitives of the vendored SDK (`fetch`,
`crypto`, `secrets`, `vars`, `tables`, `users`, `files`, `log`), and is
bundled with esbuild into `dist/workflow.js`. Every run is one call of
`run`, from start to finish. See
[design/workflows-v2.md](design/workflows-v2.md) and
[design/workflows-host-api.md](design/workflows-host-api.md).

```sh
autablectl workflow init ./nightly --database sales --name nightly --title "Nightly"
cd nightly && npm install && npm run typecheck
autablectl workflow push -m "first version"      # npm run build, upload; nothing runs yet
autablectl workflow publish sales nightly 1       # reads the triggers; this version runs
autablectl workflow run sales nightly --trigger start --input '{"note":"hi"}' --wait
autablectl workflow runs sales nightly --status failed --limit 5
autablectl workflow run-log sales nightly 42      # event, output, error, logs
autablectl workflow rollback sales nightly        # publish the newest version below the current one
autablectl workflow list sales
autablectl workflow versions sales nightly        # versions, triggers (webhook URLs), settings, secret names, vars
autablectl workflow pull sales nightly ./nightly-v1 --version 1
autablectl workflow sdk                           # after tables change or the server is upgraded
printf %s "$TOKEN" | autablectl workflow secret set sales nightly api_token
autablectl workflow secret unset sales nightly api_token
autablectl workflow var set sales nightly region eu
autablectl workflow var unset sales nightly region
autablectl workflow disable sales nightly         # triggers stop starting runs; enable undoes it
autablectl workflow delete sales nightly          # with its versions, settings, runs, and grants
```

- `workflow init` writes a TypeScript + esbuild project (`npm run build`
  bundles `src/index.ts` into `dist/workflow.js`, an IIFE assigning
  `globalThis.__workflow`; `npm run typecheck`), the manifest
  `autable.workflow.json`, the SDK from `GET /api/workflows/sdk` into
  `src/autable/`, and `src/autable/schema.ts` with the database's tables and
  fields as types. The project's `AGENTS.md` explains the rules. The
  directory must be new or empty.
- `workflow sdk` rewrites `src/autable/` from the server and updates `sdk` in
  the manifest. Do not edit those files.
- `workflow push` runs `npm run build` (skip with `--no-build`) and uploads
  the manifest, `dist/`, and the source, chosen as for apps. A push never
  changes what runs unless `--publish` is given; publishing fails when the
  trigger declarations are invalid.
- `workflow run` starts a manual trigger: `--trigger` names it (it may be
  omitted when the workflow has only one), `--input` is a JSON object. Without
  `--wait` it prints the queued run. With `--wait` it prints the finished
  run, polling past the server's 60-second wait, and exits `1` when the run
  did not succeed.
- `workflow secret set` reads the value from stdin, or prompts for it without
  echo when stdin is a terminal; it is never taken from the command line and
  never printed (the output shows only its length).
- `workflow pull` downloads the source of a version (default: the published
  one, else the newest) into a new or empty directory.
- `workflow var set` takes the value as an argument; put `--` before a value
  that starts with `-` (a PEM public key, for example).

### Apps

An app is an ordinary frontend project that autable hosts at
`/apps/<database>/<name>/` and that uses the HTTP API with the permissions of
whoever opens it. See [design/apps.md](design/apps.md). `init`, `sdk`, and
`dev` work for any user; `push`, `publish`, `rollback`, and `pull` need an admin
account.

```sh
autablectl app init ./orders --database sales --name orders --title "Orders"
cd orders && npm install
autablectl app dev                       # npm run dev, /api proxied to the server as you
autablectl app push -m "first version"   # npm run build, upload; prints the preview URL
autablectl app publish sales orders 1
autablectl app rollback sales orders     # publish the newest version below the current one
autablectl app list sales
autablectl app versions sales orders
autablectl app pull sales orders ./orders-v1 --version 1
autablectl app sdk                       # after tables change or the server is upgraded
```

- `app init` writes a Vite + React + TypeScript project (react-router, with
  the router base name taken from the SDK's `basePath()`), the manifest
  `autable.app.json`, the SDK from `GET /api/apps/sdk` into `src/autable/`,
  and `src/autable/schema.ts` with the database's tables and fields as types.
  The directory must be new or empty.
- `app sdk` rewrites `src/autable/` from the server and updates `sdk` in the
  manifest. Do not edit those files.
- `app dev` runs `npm run dev` with `AUTABLE_SERVER` and `AUTABLE_TOKEN` set
  from your login; the template's `vite.config.ts` proxies `/api` there with
  the token.
- `app push` runs `npm run build` (skip with `--no-build`) and uploads the
  manifest, `dist/`, and the source. Source files are those
  `git ls-files --cached --others --exclude-standard` lists when the project is
  in a git work tree, otherwise everything except `node_modules`, `dist`,
  `.git`, and `.autable` directories. A push never changes what users see
  unless `--publish` is given.
- `app pull` downloads the source of a version (default: the published one,
  else the newest) into a new or empty directory.

### Agent skill

```sh
autablectl skill install                  # ~/.claude/skills/autable/SKILL.md
autablectl skill install --dir ./skills/autable
autablectl skill show                     # print it
```

The skill is a page of rules for changing a live server plus a map of where
each behavior lives in the source, linked at the release matching the binary.
It deliberately does not restate behavior: the source is the reference.

### Files and raw API access

```sh
autablectl file upload ./contract.pdf --database sales --table orders --record 42
autablectl file download 17 --output ./contract.pdf
autablectl api GET /api/metadata
autablectl api POST /api/tables/sales/orders/rows/query --data @query.json
```

`api` sends any request with the stored token and prints the JSON response;
use it for endpoints without a dedicated command.
