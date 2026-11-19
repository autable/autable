# Working on this autable app

This project is hosted by an autable server. `autable.app.json` names the
database and the app; `autablectl` (signed in with `autablectl login`) builds
it, uploads it, and publishes it.

## Loop

1. `npm install` once.
2. `autablectl app dev` runs `npm run dev` with `/api` proxied to the
   server under your login. Open the printed local URL.
3. Edit `src/`. `npm run typecheck` must pass; `npm run build` runs it too.
4. `autablectl app push -m "what changed"` builds, uploads a new version,
   and prints its preview URL (`/apps/<database>/<name>/@<version>/`).
   Pushing never changes what users see.
5. Check the preview, then `autablectl app publish <database> <name> <version>`
   (or push with `--publish`). `autablectl app rollback <database> <name>`
   publishes the version before the current one.

`autablectl app versions <database> <name>` lists versions; `autablectl app
pull <database> <name> <dir>` downloads the source of any version.

## The SDK

- `src/autable/` is vendored by `autablectl app sdk`: `autable.ts` is the SDK
  and `schema.ts` holds the tables and fields of the database as types. Do
  not edit them; run `autablectl app sdk` after the tables change or the
  server is upgraded. It also updates `sdk` in `autable.app.json`.
- Talk to autable only through the SDK (`table(...)`, `files`,
  `currentUser`, `searchUsers`, `runWorkflow`). It sends the SDK version
  with every request; a server that no longer supports it answers 410 and
  the SDK throws an error asking for a rebuild.
- Every request runs with the signed-in user's permissions. Hiding
  something in the UI protects nothing; grants in autable do.
- Pass `basePath()` to the router (already done in `src/main.tsx`) and keep
  `base: "./"` in `vite.config.ts`: the same build is served at the
  published address and at every preview address.
- `npm run build` must produce `dist/index.html`. The source pushed with
  each version is what git would commit (or, outside git, everything except
  `node_modules`, `dist`, `.git`, and `.autable`); keep secrets out of it.
