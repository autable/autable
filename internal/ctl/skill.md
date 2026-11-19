---
name: autable
description: Read and change a live autable server (tables, rows, workflows, hosted apps) with autablectl. Use when a task involves autable data, workflow scripts, or building an autable app.
---

# Working with autable

autable is a code-first table/workflow server that also hosts web apps. This
skill is a map, not a manual: the source code is the reference for every
behavior. When something is not covered here, read the source at the version
you are talking to:

**Source: {{SOURCE}}** (autablectl {{VERSION}}; the server should run the same release)

## Rules

- Change the live server only through `autablectl`. The git repository autable
  pushes metadata and workflow scripts to is a one-way backup; editing it
  changes nothing.
- autable is admin-gated. Only admins (emails listed under `auth.admins` in
  the server config) change code or structure: databases, tables, fields,
  views, permissions, workflows, and apps. Everyone else reads and writes data
  within their grants, opens apps, and runs workflows. `autablectl whoami`
  shows whether you are signed in as an admin; if you are not, a 403 on these
  commands is final — hand the change to an admin instead of working around it.
- Discover before you write: `autablectl meta [DB [TABLE]]` for tables, fields
  (types, enum `options`, `deleted`), and views; `autablectl nodes` for every
  workflow node with its inputs, outputs, instance variables, secrets, and
  documentation;
  `autablectl help` and `autablectl <command> -h` for the CLI.
- Edit workflow scripts with the pull loop: `autablectl pull` into a
  directory, edit `workflow/<db>/<name>.js`, `autablectl status`,
  `autablectl push --dry-run`, `autablectl push`. Push refuses when the
  workflow changed on the server since the last pull; look at the server copy
  (`workflow get --script`), merge, then `push --force`. `workflow set` also
  counts as a server change.
- Build UIs as apps: ordinary frontend projects autable hosts at
  `/apps/<db>/<name>/`. `autablectl app init DIR --database DB --name NAME`
  scaffolds Vite + React + TypeScript with the SDK in `src/autable/`; read the
  project's `AGENTS.md`. Loop: `npm install`, `autablectl app dev`, edit,
  `npm run build` (it type-checks), `autablectl app push -m "..."`, open the
  preview URL it prints, then `autablectl app publish DB NAME VERSION`.
  `app rollback` undoes a publish; `app pull` fetches the source of any
  version, so take over an existing app with it instead of starting over.
- Never edit `src/autable/` in an app: run `autablectl app sdk` after tables
  or fields change or the server is upgraded. Talk to autable only through the
  SDK; it sends the SDK version the server checks.
- Workflows run with their own identity; apps run in the viewer's browser with
  the viewer's permissions. Hiding a column in an app does not protect it;
  field permissions do.
- Secrets are never readable through the API (only their lengths). Ask a human
  to set them in the web UI.
- After changing a workflow, run it (`workflow run`, then `workflow run-log`)
  and check its output before reporting success. Messages a workflow sends
  are real. After pushing an app, open the preview before publishing.

## Where to look in the source

| Question | Look at |
| --- | --- |
| Workflow script contract (`instances`, `trigger`, `run`, `info.instance(name).exec`) | `internal/workflow/runner.go`, type hints in `web/src/editorTypes.ts` |
| What a node does, its params and errors | `internal/workflow/nodes/<path>/` (many have a `docs/` folder); table nodes are served by `internal/api/workflow_table_nodes.go` |
| Apps: model, bundle, SDK versioning, URLs, CLI | `docs/design/apps.md` |
| App SDK (`table(...)`, `files`, `currentUser`, `runWorkflow`, `basePath`) | `internal/appsdk/files/autable.ts`; field types in `schema.ts` come from `internal/ctl/appschema.go` |
| App project template | `internal/ctl/apptemplate/` |
| App serving, upload, publish | `internal/api/apps.go`, `internal/appbundle/bundle.go`, `internal/appsdk/sdk.go` |
| Query and sort syntax (`{combinator, rules}`, operators), field types, views | `internal/metadata/metadata.go`, `internal/recorddb/repository.go` |
| Permissions (grant scopes, levels, roles) | `docs/permissions.md`, `internal/api/authorization.go` |
| HTTP endpoints (for `autablectl api` and apps) | `routes()` in `internal/api/server.go` |
| CLI behavior | `docs/cli.md`, `internal/ctl/` |
| Existing workflows and apps on the server | `autablectl pull`, `autablectl app list DB` — follow the conventions you find there |
