# autablectl

`autablectl` is the command-line client for a running autable server. It talks
to the same HTTP API as the web UI, with the permissions of the user who signed
it in, so scripts and coding agents can read and change live tables,
workflows, and forms instead of editing a repository checkout (the git
repository autable pushes to is a backup; edits made there are never pulled
back).

Every command prints JSON on stdout and diagnostics on stderr. Exit codes:
`0` success, `1` the request failed, `2` the command line was invalid.

Release archives named `autable-<version>-cli-<os>-<arch>` contain the binary
for Linux (amd64, arm64), Windows (amd64), and macOS (arm64). To build it
yourself: `CGO_ENABLED=0 go build ./cmd/autablectl`.

## Signing in

```sh
autablectl login --server https://autable.example.com
```

`login` opens the browser at `/api/auth/cli/authorize`. If the browser is not
signed in it goes through the normal login page (password or OIDC) first; the
user then confirms on a page naming the account. The server redirects to a
loopback port the CLI listens on with a one-time code, and the CLI exchanges
the code, together with a PKCE verifier only it knows, for a session token.

- The token is an ordinary session lasting 90 days, stored in
  `<user config dir>/autable/autablectl.json` (mode 0600). It carries exactly
  the user's permissions.
- `autablectl logout` revokes it on the server and deletes the file.
- `--no-browser` prints the URL instead of opening it; the browser must run on
  the same computer, since the redirect goes to `127.0.0.1`.
- `--token TOKEN` stores an existing session token instead.
- `AUTABLE_SERVER` and `AUTABLE_TOKEN` override the stored login for a single
  invocation.

Any session token works as `Authorization: Bearer <token>` against the API, so
other HTTP clients can use the same token.

## Commands

Run `autablectl help` for the full list and `autablectl <command> -h` for a
command's flags. JSON-valued flags (`--values`, `--query`, `--inputs`,
`--fields`, `--data`) accept inline JSON, `@path` to read a file, or `-` to
read stdin.

### Structure

```sh
autablectl meta                      # every readable database, table, field, view
autablectl meta sales orders         # one table
autablectl fields add sales orders --fields '[{"name":"数量","type":"int"}]'
autablectl nodes                     # workflow node types and their params
```

### Rows

```sh
autablectl rows list sales orders --query '{"field":"状态","operator":"=","value":"open"}' --limit 20
autablectl rows list sales orders --query '{"combinator":"or","rules":[...]}' --sorts '[{"field":"ct_record_id","direction":"desc"}]'
autablectl rows get sales orders 42
autablectl rows create sales orders --values '{"名称":"Widget","数量":3}'
autablectl rows update sales orders 42 --values '{"数量":4}'
autablectl rows upsert sales orders --match 名称 --values '{"名称":"Widget","数量":5}'
autablectl rows delete sales orders 42
autablectl rows history sales orders 42
```

`rows list` prints `{"rows": [...], "total": N}`. A `--query` with a single
`field` is wrapped into an `and` group.

### Workflows and forms

```sh
autablectl workflow list sales
autablectl workflow get sales notify            # settings; secrets show only their lengths
autablectl workflow get sales notify --script   # just the source
autablectl workflow set sales notify --var recipient=alice --unset-var old --enabled true --timeout 120
autablectl workflow run sales notify --inputs '{"record_id":42}'
autablectl workflow runs sales notify --limit 5
autablectl workflow run-log sales notify <history_key>
autablectl form list sales
autablectl form get sales intake --script
```

Workflows and forms are addressed by name, or by id. `workflow run` exits `1`
when the run fails and still prints the run record with its error.

### Editing scripts: pull, status, push

```sh
mkdir scripts && cd scripts
autablectl pull                 # all readable databases; --database to narrow
$EDITOR workflow/sales/notify.js
autablectl status               # what changed locally
autablectl push --dry-run
autablectl push
```

`pull` writes `workflow/<database>/<name>.js` and `form/<database>/<name>.js`,
the same layout the server uses in its repository, and records in
`.autable/state.json` which object each file belongs to, the object's
`updated_at`, and the file's hash.

- `push` saves only files whose content changed. A workflow push replaces the
  script and sends the current settings back unchanged; secrets are never sent
  or overwritten.
- A new file `workflow/<database>/<name>.js` creates a workflow named
  `<name>` (likewise for forms). The directory must match a database you can
  read.
- If the object changed on the server since the last pull (someone edited it
  in the web UI, or `workflow set` ran), `push` refuses with a conflict. `pull`
  never overwrites local edits, so: inspect the server version with
  `workflow get --script`, merge, then `push --force`.
- Deleting a file does not delete anything on the server.

### Files and raw API access

```sh
autablectl file upload ./contract.pdf --database sales --table orders --record 42
autablectl file download 17 --output ./contract.pdf
autablectl api GET /api/metadata
autablectl api POST /api/tables/sales/orders/rows/query --data @query.json
```

`api` sends any request with the stored token and prints the JSON response;
use it for endpoints without a dedicated command.
