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
for Linux (amd64) and Windows (amd64). To build it
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
  the manifest, `dist/`, and the source, chosen as for apps (including
  shared files outside the project, see below). A push never
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

### Libraries

A library is shared TypeScript that workflows (and other libraries)
import as `@lib/<name>`. It belongs to no database. Every workflow run
loads the library's **published** version, so publishing or rolling back
a library changes every importing workflow at its next run, without
pushing the workflows. All `lib` commands need an admin account. See
"Libraries" in [design/workflows-v2.md](design/workflows-v2.md).

```sh
autablectl lib init ./http --name http --title "HTTP helpers"
cd http && npm install && npm run build
autablectl lib push -m "first version"           # npm run build, upload; nothing changes yet
autablectl lib publish http 1                    # importing workflows use it from their next run
autablectl lib rollback http                     # publish the newest version below the current one
autablectl lib list                              # with the workflows and libraries importing each
autablectl lib versions http                     # versions, imports, imported_by
autablectl lib pull http ./http-v1 --version 1
autablectl lib sdk                               # refresh the vendored SDK and library types
autablectl lib delete http                       # refused while a published version imports it
```

- `lib init` writes a TypeScript + esbuild project: `npm run build` emits
  the declarations with `tsc`, bundles `src/index.ts` into `dist/lib.js`
  (an IIFE assigning `__lib`), and joins the declarations into
  `dist/index.d.ts` as `declare module "@lib/<name>"`. The manifest is
  `autable.lib.json` (`name`, `title`, `sdk`; no database). `src/autable/`
  holds the workflow SDK and a `schema.ts` without tables.
- `workflow init` and `workflow sdk` (and `lib init`/`lib sdk`) vendor the
  declarations of every published library into
  `src/autable/libs/<name>.d.ts` and print them as `libraries`; run
  `workflow sdk` again after a library publishes a new API. They skip
  this with a note on stderr when the server cannot list libraries.
- `lib push --publish` and `lib publish` fail when the library imports a
  library that is not published, imports itself, or would close an import
  cycle. Publishing a workflow that imports a missing or unpublished
  library fails the same way.

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

### Shared code in a development repository

Apps and workflows can live in one git repository with shared code beside
them, which every project imports by relative path:

```
repo/
  lib/notify.ts                 import { notify } from "../../lib/notify"
  workflows/order-sync/         autable.workflow.json, src/, build.mjs
  apps/board/                   autable.app.json, src/, vite.config.ts
```

- The builds of the current templates write `.autable/build-inputs.json`,
  the files the bundle was built from (`build.mjs` from esbuild's metafile;
  a small plugin in `vite.config.ts` from the module ids of the Vite
  build), as paths relative to the project.
- `push` uploads the project's source as above. When the build used files
  outside the project, it also uploads exactly those files, never anything
  below `node_modules` and nothing else of the repository. The source then
  mirrors the repository from the root of the git work tree (`lib/...`,
  `workflows/order-sync/...`), and the uploaded manifest gets `"root":
  "workflows/order-sync"`, the project's path in it. `push` prints `root`
  and `shared_files`. A project not inside a git work tree may not use
  files outside its directory; `push` refuses with the paths it found.
- `pull` writes such a version as that repository slice: it prints `root`
  and `project_dir` (`DIR/<root>`), where the project's own manifest is;
  `npm install && npm run build` there builds it as before.
- Shared code imports the SDK as `@autable/workflow` or `@autable/app`,
  which the templates' `tsconfig.json` and build configs map to the
  project's vendored SDK. npm packages that shared code imports resolve from
  its own directory, so install them at the repository root.
- Only modules are recorded for apps: a stylesheet outside the project must
  be imported from TypeScript, and its own `@import`s and `url()`s of
  further outside files are not uploaded.
- A project whose build writes no build-inputs file (made from an older
  template) uploads only its directory; `push` notes that on stderr.

### Agent skill

```sh
autablectl skill export ./autable-skill            # write the package into a new or empty directory
autablectl skill export ./autable-skill --force    # replace a previously exported package
autablectl skill export --tar autable-skill.tar.gz # the same package as a .tar.gz
autablectl skill install                           # into ~/.claude/skills/autable
autablectl skill install ./skills/autable          # into another skills directory
autablectl skill show                              # print SKILL.md
```

The agent skill is a self-contained package for coding agents that cannot
see autable's source:

- `SKILL.md` at the root: the rules for changing a live server (admin gate,
  discover first, secrets, verify before reporting), the working loops for
  workflows, apps, and libraries, and an index of the reference files.
- `reference/`: this documentation (`cli.md`, `query.md`, `http-api.md`,
  `permissions.md`, `design/*.md`), the app SDK (`app-sdk/`) and workflow
  SDK (`workflow-sdk/`) sources exactly as `app sdk` and `workflow sdk`
  vendor them, and the `AGENTS.md` of the app, workflow, and library
  project templates (`templates/`).
- `VERSION`, and a line at the top of `SKILL.md`, name the autablectl
  release that wrote it. Every file is generated from what is compiled into
  the binary, so it matches that release; export it again after upgrading.

`export` refuses a non-empty directory unless `--force`, which replaces
`SKILL.md`, `VERSION`, and `reference/` and leaves other files alone.
`install` does the same into a directory that is empty or already holds the
autable skill, and refuses any other. The package is a few hundred KB, has
no scripts, and contains no inline shell commands.

To distribute the skill to a team, upload the exported directory (or the
`.tar.gz`, whose root is the package) to a skill-distribution system that
accepts a folder with `SKILL.md` at its root, for example a team policy that
assembles uploaded skills into the agents' plugins. Re-export and upload it
again after each autablectl upgrade so the reference matches the server.

### Files and raw API access

```sh
autablectl file upload ./contract.pdf --database sales --table orders --record 42
autablectl file download 17 --output ./contract.pdf
autablectl api GET /api/metadata
autablectl api POST /api/tables/sales/orders/rows/query --data @query.json
```

`api` sends any request with the stored token and prints the JSON response;
use it for endpoints without a dedicated command.
