# __APP_NAME__

An autable app: a Vite + React + TypeScript project that autable hosts at
`/apps/<database>/<name>/`. It reads and writes tables through autable's API
with the permissions of whoever opens it.

```sh
npm install
autablectl app dev                  # http://localhost:5173, /api proxied to autable as you
autablectl app push -m "what changed"   # build, upload, print the preview URL
autablectl app publish <database> <name> <version>
```

Code shared with other apps and workflows of the same git repository (a
`lib/` next to them) is bundled in and uploaded with the source; see
"Shared code" in AGENTS.md.

See AGENTS.md for the full loop and the rules.
