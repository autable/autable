---
name: autable
description: Read and change a live autable server with autablectl - tables, fields, views, rows and queries, permissions, workflows (TypeScript automation with schedule, recordChanged, webhook and manual triggers), libraries (@lib/ shared code), and hosted web apps built on the autable app SDK. Use whenever a task mentions autable or autablectl, reads or writes autable data, or builds, debugs, deploys, or takes over an autable workflow, library, or app.
---

# Working with autable

Skill package from autablectl {{VERSION}} (compare `autablectl version`).
The server should run the same release; export or install the package again
after upgrading autablectl.

autable is a code-first table/workflow server that also hosts web apps. You
work with it through `autablectl`, which prints JSON and acts with the
signed-in user's permissions. This file holds the rules and the working
loops; the `reference/` files beside it hold the details (see the index at
the end). Read the reference file that answers your question instead of
guessing, and trust what the live server tells you (`autablectl meta`,
`autablectl help`, `autablectl <command> -h`) over memory.

## Rules

- Change the live server only through `autablectl`. The git repository autable
  pushes to (metadata, plus the source of every pushed app, workflow and
  library version under `app/`, `workflow/`, `lib/`) is a one-way backup:
  read it for history, but editing it changes nothing. Develop in your own
  repository instead.
- autable is admin-gated. Only admins (emails listed under `auth.admins` in
  the server config) change code or structure: databases, tables, fields,
  views, permissions, workflows, libraries, and apps. Everyone else reads and
  writes data within their grants, opens apps, and runs workflows.
  `autablectl whoami` shows whether you are signed in as an admin; if you are
  not, a 403 on these commands is final.
- If you are not an admin, prepare the change (code in the development
  repository, a description of schema or permission changes) and hand it to
  an admin to deploy; admins are the deployment gate, and review happens
  outside autable.
- Discover before you write: `autablectl meta [DB [TABLE]]` for tables,
  fields (types, enum `options`, `deleted`), and views; `autablectl help` and
  `autablectl <command> -h` for the CLI. Copy field names exactly; they are
  often non-ASCII.
- For one-off data work use `autablectl rows list|get|create|update|upsert|
  delete|history` (and `autablectl api METHOD PATH` for any other endpoint);
  every call is checked against your own grants. Query, sort and value
  formats are in `reference/query.md`. Anything that has to happen again
  belongs in a workflow, not in a script you run by hand.
- Secrets are never readable through the API. Ask a human for the value, or
  pipe it in: `autablectl workflow secret set DB NAME KEY` reads stdin; never
  put a secret on a command line, in source, or in a log.
- Verify before reporting. After changing a workflow, run it (`workflow run
  --wait`, then `workflow run-log`) and check its output; messages and
  writes a workflow makes are real. After pushing an app, open the preview
  URL before publishing. After writing rows, read them back.

## Workflows

Automation is a workflow: a TypeScript project bundled into one script and
versioned like an app.

1. `autablectl workflow init DIR --database DB --name NAME` scaffolds it with
   the SDK in `src/autable/`; read the project's `AGENTS.md` (the same text
   is in `reference/templates/workflow/AGENTS.md`).
2. `npm install`, edit `src/index.ts`, `npm run typecheck`.
3. `autablectl workflow push -m "..."` uploads a version; nothing runs yet.
4. `autablectl workflow publish DB NAME VERSION` makes it the one that runs.
5. `autablectl workflow run DB NAME --wait`, then `autablectl workflow
   run-log DB NAME RUN_ID`.

`workflow rollback` undoes a publish; `workflow pull` fetches the source of
any version, so take over an existing workflow with it instead of starting
over. `workflow versions DB NAME` shows triggers (with webhook URLs),
settings, secret names, and vars.

- Workflows are stateless: a run is one call of `run(event)`. Integrations
  are plain code on the SDK's `fetch` (with `via` for runners) and `crypto`;
  there are no nodes. Processes that span time keep their state in table
  rows: a `schedule` trigger polls the other system and writes results into
  rows, and `recordChanged` triggers move each stage on.
- Workflows run as their own subject, `workflow:<id>`, with the grants an
  admin gave it, not as the person who pushed them.

## Apps

UIs are apps: ordinary frontend projects autable hosts at
`/apps/<db>/<name>/`.

1. `autablectl app init DIR --database DB --name NAME` scaffolds Vite + React
   + TypeScript with the SDK in `src/autable/`; read the project's
   `AGENTS.md` (the same text is in `reference/templates/app/AGENTS.md`).
2. `npm install`, `autablectl app dev` (local dev server with `/api` proxied
   as you), edit, `npm run build` (it type-checks).
3. `autablectl app push -m "..."`, then open the preview URL it prints.
4. `autablectl app publish DB NAME VERSION`.

`app rollback` undoes a publish; `app pull` fetches the source of any
version, so take over an existing app with it instead of starting over.

- Apps run in the viewer's browser with the viewer's permissions. Hiding a
  column in an app does not protect it; field permissions do. An app's
  `runWorkflow` starts a workflow's `manual` trigger, which needs the viewer
  to hold a run grant on that workflow.

## SDK vendoring

- Never edit `src/autable/` in an app, workflow, or library: run
  `autablectl app sdk`, `autablectl workflow sdk`, or `autablectl lib sdk`
  after tables or fields change, a library publishes a new API, or the
  server is upgraded. It rewrites the SDK and `schema.ts` (the database's
  tables and fields as types).
- Talk to autable only through the SDK. A server that no longer supports a
  project's SDK version answers 410; refresh the SDK and rebuild.
- `reference/app-sdk/` and `reference/workflow-sdk/` hold the same SDK files
  `sdk` vendors, with the typed API and its doc comments.

## Libraries and shared code

- Libraries (`@lib/<name>`) are instance-wide shared code that admins push
  with `autablectl lib init|push|publish|rollback`; every workflow run loads
  a library's published version, so publishing one changes every importing
  workflow at its next run (`lib versions NAME` lists the importers; `lib
  rollback` undoes it instantly). Keep library exports backward compatible,
  push without `--publish` and test first, and run `workflow sdk` in
  importing projects for the types. A run's `libraries` show the versions
  it loaded.
- Repository-level shared code: write integrations and helpers once, in the
  development repository's shared `lib/` beside its `workflows/` and
  `apps/`, imported by relative path; shared code imports the SDK as
  `@autable/workflow` or `@autable/app`. `push` uploads the shared files the
  build used with the source (the project's path in the repository becomes
  `root`), and `pull` recreates that slice; the project is at the
  `project_dir` it prints. See "Shared code" in `reference/cli.md`.

## Existing work on the server

`autablectl workflow list DB`, `autablectl app list DB`, and `autablectl lib
list` show what exists; `workflow pull`, `app pull`, and `lib pull` fetch
its source. Follow the conventions you find there.

## Reference index

Paths are relative to this file.

| Question | Read |
| --- | --- |
| CLI commands and flags, sign-in, shared code, output shapes | `reference/cli.md` |
| Field types and JSON values, query rules and operators, sorts, views, `$current_user`, paging, upsert, files, relations, formulas, system columns | `reference/query.md` |
| HTTP routes, bearer token vs cookie, CSRF rule, status codes (for `autablectl api`) | `reference/http-api.md` |
| Grants, scopes, levels, roles: who may do what | `reference/permissions.md` |
| Apps: model, bundle, preview and published URLs, SDK versioning | `reference/design/apps.md` |
| Workflows: triggers, runs, limits, libraries, settings, secrets, vars | `reference/design/workflows-v2.md` |
| What workflow code may call, event shapes, webhook responses | `reference/design/workflows-host-api.md` |
| Remote runners (`fetch` with `via`) | `reference/design/remote-runner.md` |
| App SDK (`table(...)`, `files`, `currentUser`, `searchUsers`, `runWorkflow`, `basePath`) | `reference/app-sdk/autable.ts` |
| Workflow SDK (triggers, `fetch`, `crypto`, `tables`, `users`, `files`, `secrets`, `vars`, `log`) | `reference/workflow-sdk/` (start at `index.ts`) |
| Project rules that `init` writes | `reference/templates/app/AGENTS.md`, `reference/templates/workflow/AGENTS.md`, `reference/templates/lib/AGENTS.md` |
