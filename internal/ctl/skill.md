---
name: autable
description: Read and change a live autable server (tables, rows, workflows, forms) with autablectl. Use when a task involves autable data, workflow scripts, or form scripts.
---

# Working with autable

autable is a code-first table/workflow/form server. This skill is a map, not a
manual: the source code is the reference for every behavior. When something
is not covered here, read the source at the version you are talking to:

**Source: {{SOURCE}}** (autablectl {{VERSION}}; the server should run the same release)

## Rules

- Change the live server only through `autablectl`. The git repository autable
  pushes metadata and scripts to is a one-way backup; editing it changes
  nothing.
- Discover before you write: `autablectl meta [DB [TABLE]]` for tables, fields
  (types, enum `options`, `deleted`), and views; `autablectl nodes` for every
  workflow node with its inputs, outputs, instance variables, secrets, and
  documentation;
  `autablectl help` and `autablectl <command> -h` for the CLI.
- Edit scripts with the pull loop: `autablectl pull` into a directory, edit
  `workflow/<db>/<name>.js` or `form/<db>/<name>.js`, `autablectl status`,
  `autablectl push --dry-run`, `autablectl push`. Push refuses when the object
  changed on the server since the last pull; look at the server copy
  (`workflow get --script` / `form get --script`), merge, then `push --force`.
  `workflow set` also counts as a server change.
- Workflows run with their own identity, forms run in the viewer's browser with
  the viewer's permissions. Hiding a column in a form does not protect it;
  field permissions do.
- Secrets are never readable through the API (only their lengths). Ask a human
  to set them in the web UI.
- After changing a workflow, run it (`workflow run`, then `workflow run-log`)
  and check its output before reporting success. Messages a workflow sends
  are real.

## Where to look in the source

| Question | Look at |
| --- | --- |
| Workflow script contract (`instances`, `trigger`, `run`, `info.instance(name).exec`) | `internal/workflow/runner.go`, type hints in `web/src/editorTypes.ts` |
| What a node does, its params and errors | `internal/workflow/nodes/<path>/` (many have a `docs/` folder); table nodes are served by `internal/api/workflow_table_nodes.go` |
| Form script API (`render(api, root)`, `api.input/select/relation/file/button`, `api.rows`, `api.show`, `api.pdf`) | `web/src/formRuntime.ts`, `web/src/hooks/useFormRunner.ts`, result rendering in `web/src/components/FormPreviewFields.tsx` |
| Query and sort syntax (`{combinator, rules}`, operators), field types, views | `internal/metadata/metadata.go`, `internal/recorddb/repository.go` |
| Permissions (grant scopes, levels, roles, published forms) | `docs/permissions.md`, `internal/api/authorization.go` |
| HTTP endpoints (for `autablectl api`) | `routes()` in `internal/api/server.go` |
| CLI behavior | `docs/cli.md`, `internal/ctl/` |
| Existing scripts on the server | `autablectl pull` — follow the conventions you find there |
