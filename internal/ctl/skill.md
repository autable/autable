---
name: autable
description: Read and change a live autable server (tables, rows, workflows, hosted apps) with autablectl. Use when a task involves autable data, building or running an autable workflow, or building an autable app.
---

# Working with autable

autable is a code-first table/workflow server that also hosts web apps. This
skill is a map, not a manual: the source code is the reference for every
behavior. When something is not covered here, read the source at the version
you are talking to:

**Source: {{SOURCE}}** (autablectl {{VERSION}}; the server should run the same release)

## Rules

- Change the live server only through `autablectl`. The git repository autable
  pushes metadata to is a one-way backup; editing it changes nothing.
- autable is admin-gated. Only admins (emails listed under `auth.admins` in
  the server config) change code or structure: databases, tables, fields,
  views, permissions, workflows, and apps. Everyone else reads and writes data
  within their grants, opens apps, and runs workflows. `autablectl whoami`
  shows whether you are signed in as an admin; if you are not, a 403 on these
  commands is final — hand the change to an admin instead of working around it.
- Discover before you write: `autablectl meta [DB [TABLE]]` for tables, fields
  (types, enum `options`, `deleted`), and views; `autablectl help` and
  `autablectl <command> -h` for the CLI.
- Build automation as workflows: TypeScript projects bundled into one script
  and versioned like apps. `autablectl workflow init DIR --database DB --name
  NAME` scaffolds one with the SDK in `src/autable/`; read the project's
  `AGENTS.md`. Loop: `npm install`, edit `src/index.ts`, `npm run typecheck`,
  `autablectl workflow push -m "..."`, `autablectl workflow publish DB NAME
  VERSION`, then `autablectl workflow run DB NAME --wait` and `workflow
  run-log`. `workflow rollback` undoes a publish; `workflow pull` fetches the
  source of any version, so take over an existing workflow with it.
- Workflows are stateless: a run is one call of `run(event)`. Integrations
  are plain code on the SDK's `fetch` (with `via` for runners) and `crypto`;
  there are no nodes. Processes that span time keep their state in table
  rows: a `schedule` trigger polls the other system and writes results into
  rows, and `recordChanged` triggers move each stage on.
- Build UIs as apps: ordinary frontend projects autable hosts at
  `/apps/<db>/<name>/`. `autablectl app init DIR --database DB --name NAME`
  scaffolds Vite + React + TypeScript with the SDK in `src/autable/`; read the
  project's `AGENTS.md`. Loop: `npm install`, `autablectl app dev`, edit,
  `npm run build` (it type-checks), `autablectl app push -m "..."`, open the
  preview URL it prints, then `autablectl app publish DB NAME VERSION`.
  `app rollback` undoes a publish; `app pull` fetches the source of any
  version, so take over an existing app with it instead of starting over.
- Never edit `src/autable/` in an app or workflow: run `autablectl app sdk`
  or `autablectl workflow sdk` after tables or fields change or the server is
  upgraded. Talk to autable only through the SDK.
- Workflows run as their own subject, `workflow:<id>`, with the grants an
  admin gave it; apps run in the viewer's browser with the viewer's
  permissions. Hiding a column in an app does not protect it; field
  permissions do.
- Secrets are never readable through the API. Ask a human for the value, or
  pipe it in: `autablectl workflow secret set DB NAME KEY` reads stdin; never
  put a secret on a command line, in source, or in a log.
- After changing a workflow, run it (`workflow run --wait`, then `workflow
  run-log`) and check its output before reporting success. Messages and
  writes a workflow makes are real. After pushing an app, open the preview
  before publishing.

## Where to look in the source

| Question | Look at |
| --- | --- |
| Workflows: model, triggers, runs, limits, API, CLI | `docs/design/workflows-v2.md` |
| What workflow code may call, event shapes, webhook responses | `docs/design/workflows-host-api.md` |
| Workflow SDK (`schedule`, `recordChanged`, `webhook`, `manual`, `fetch`, `crypto`, `tables`, `secrets`, `log`) | `internal/wfsdk/files/` (start at `index.ts`) |
| Workflow project template | `internal/ctl/wftemplate/` |
| How a run executes (host functions, event loop, timeouts) | `internal/wfruntime/` |
| Apps: model, bundle, SDK versioning, URLs, CLI | `docs/design/apps.md` |
| App SDK (`table(...)`, `files`, `currentUser`, `runWorkflow`, `basePath`) | `internal/appsdk/files/autable.ts`; field types in `schema.ts` (apps and workflows) come from `internal/ctl/appschema.go` |
| App project template | `internal/ctl/apptemplate/` |
| App serving, upload, publish | `internal/api/apps.go`, `internal/appbundle/bundle.go`, `internal/appsdk/sdk.go` |
| Query and sort syntax (`{combinator, rules}`, operators), field types, views | `internal/metadata/metadata.go`, `internal/recorddb/repository.go` |
| Permissions (grant scopes, levels, roles) | `docs/permissions.md`, `internal/api/authorization.go` |
| HTTP endpoints (for `autablectl api` and apps) | `routes()` in `internal/api/server.go` |
| CLI behavior | `docs/cli.md`, `internal/ctl/` |
| Existing workflows and apps on the server | `autablectl workflow list DB`, `autablectl app list DB`, then `workflow pull` / `app pull` — follow the conventions you find there |
