# Working on this autable library

This project is a library of an autable server: shared code that workflows
(and other libraries) import as `@lib/<name>`, for example the client of
another system that several workflows call. `autable.lib.json` names it.
Libraries are instance-wide: they belong to no database, and only admins
push, publish, or delete them.

## Publishing affects every importer immediately

A workflow never contains the library: each run loads the library's
**published** version. Publishing a version (`autablectl lib publish <name>
<version>` or `push --publish`) therefore changes every workflow that
imports it, from its next run on, without pushing those workflows. That is
the point (fix an integration once), and the risk:

- Keep the API backward compatible. Add functions and optional parameters;
  do not rename, remove, or change what existing exports return. For an
  incompatible change, publish a new library under a new name (`<name>-v2`)
  and move workflows over one by one.
- Test before publishing: `autablectl lib push` uploads without publishing.
  Point a test workflow at the new behaviour (for example a copy of the
  code bundled into it), run it with `autablectl workflow run ... --wait`,
  then publish. `autablectl lib versions <name>` lists the workflows and
  libraries that import it (`imported_by`): run one of them after
  publishing and check its run (`workflow run-log`); its `libraries` show
  the version it loaded.
- Rolling back is instant: `autablectl lib rollback <name>` publishes the
  version before the current one.
- A library cannot be deleted while a published workflow or library version
  imports it. Publishing fails when the library imports a library that is
  not published, or when imports would form a cycle.

## Loop

1. `npm install` once.
2. Edit `src/`. `src/index.ts` is the public API: what it exports is what
   `import { ... } from "@lib/<name>"` gives. Doc comments travel to
   importers through the type declarations.
3. `npm run build` type-checks, writes the declarations, bundles
   `src/index.ts` into `dist/lib.js` (an IIFE assigning `__lib`), and joins
   the declarations into `dist/index.d.ts`.
4. `autablectl lib push -m "what changed"`, test, `autablectl lib publish
   <name> <version>`.
5. Workflows (and libraries) that import it run `autablectl workflow sdk`
   (or `lib sdk`) to get the published types in `src/autable/libs/`.

`autablectl lib pull <name> <dir>` downloads the source of any version.

## How a library runs

- It runs inside the importing workflow's run, in the same runtime: the
  same SDK primitives (`fetch`, `crypto`, `log`, ...), and `secrets` and
  `vars` are the **calling workflow's**. Prefer taking credentials as
  parameters (the workflow reads them with `secrets.require`) so each
  workflow decides which secret to use.
- A run loads each library once, when it is first imported; every importer
  in the run shares that instance. Do not keep state in module variables
  beyond a run: nothing survives a run.
- Import the SDK as `"@autable/workflow"` (mapped to `src/autable/index.ts`
  in `tsconfig.json` and `build.mjs`), not `"./autable"`, so the published
  declarations refer to the importing workflow's SDK.
- Other libraries are imported as `"@lib/<name>"` too; they are never
  bundled in. Plain-JavaScript npm packages are bundled in.
- A library belongs to no database: `src/autable/schema.ts` has no tables.
  Take rows or table names from the workflow.
- The library's code lives in `src/` (the declarations are built from it).
- `src/autable/` is vendored by `autablectl lib sdk`; do not edit it.
