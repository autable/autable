# __WORKFLOW_TITLE__

An autable workflow: a TypeScript project that esbuild bundles into
`dist/workflow.js`, which autable runs when one of its triggers fires.

```sh
npm install
npm run typecheck
autablectl workflow push -m "what changed" --publish
autablectl workflow run <database> __WORKFLOW_NAME__ --wait
```

See AGENTS.md for the full loop and the rules.
