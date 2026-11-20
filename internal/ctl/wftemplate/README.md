# __WORKFLOW_TITLE__

An autable workflow: a TypeScript project that esbuild bundles into
`dist/workflow.js`, which autable runs when one of its triggers fires.

```sh
npm install
npm run typecheck
autablectl workflow push -m "what changed" --publish
autablectl workflow run <database> __WORKFLOW_NAME__ --wait
```

Libraries published on the server are imported as `@lib/<name>` and
loaded in their published version at every run; see "Libraries" in
AGENTS.md. Code shared with other workflows and apps of the same git repository
(a `lib/` next to them) is bundled in and uploaded with the source; see
"Shared code" in AGENTS.md.

See AGENTS.md for the full loop and the rules.
