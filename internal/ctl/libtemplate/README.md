# __LIB_TITLE__

An autable library: shared TypeScript that workflows import as
`@lib/__LIB_NAME__`. Every workflow run loads the library's published
version, so publishing a new version changes every importing workflow at
its next run, without pushing the workflows again.

```sh
npm install
npm run build
autablectl lib push -m "what changed"      # upload; nothing changes yet
autablectl lib publish __LIB_NAME__ <version>   # every importer uses it from its next run
autablectl lib rollback __LIB_NAME__            # instant undo
```

See AGENTS.md for the rules.
