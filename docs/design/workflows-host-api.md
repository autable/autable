# Workflows: host API (SDK 1.0)

Status: accepted. This is the contract between the server-side runtime
(`internal/wfruntime`) and the workflow SDK (`internal/wfsdk/files/`,
vendored into projects by `autablectl workflow sdk`). Workflow code never
touches it directly; it imports the SDK. See
[workflows-v2.md](workflows-v2.md) for the model.

The host surface is deliberately small. Everything ergonomic (`fetch`'s
`Response` wrapper, `crypto` encodings, `tables(name)` clients, trigger
helpers, request `text()`/`json()`) lives in the SDK, so the host only
changes when a new capability is added.

## Execution model

- Every run evaluates `dist/workflow.js` in a **fresh goja runtime**, then
  calls `globalThis.__workflow.run(event)` once. The run is finished when
  the returned value (or the promise it returns) settles: a run is one
  execution from start to finish, with no state carried between runs.
- The bundle must assign `globalThis.__workflow = { triggers, run }`.
  esbuild's `--format=iife --global-name=__workflow` does exactly that
  (`var __workflow = (() => { ... })()` defines a global property).
- A small **event loop** runs per execution. Host operations marked
  *async* below start a Go goroutine and return a `Promise` that settles on
  the loop. Sync operations return their result directly.
- If the run's promise is still pending while nothing is outstanding (no
  host operation, no timer), the run fails with
  `run never settles: its promise is pending with nothing left to wait for`.
- The run is aborted with `Interrupt` when the workflow's timeout (default
  10 minutes; webhook runs at most 15 s) elapses or the run is cancelled.
  In-flight host operations are cancelled with it.
- **Errors**: a host function that fails throws (sync) or rejects (async)
  with a JavaScript `Error` whose `message` is the server's explanation.
  Permission failures from `table`/`files` carry `name = "PermissionError"`;
  rows outside the subject's row set carry `name = "NotFoundError"`.
  Everything else is a plain `Error`.

### Values

Objects and arrays returned by host functions are plain JavaScript values
(object keys in sorted order). Numbers passed in `values` are written as
the field's type (int fields need integral numbers).

### Bytes

Binary data crosses the boundary as **`ArrayBuffer`**. Wherever the host
accepts bytes it also accepts any `ArrayBufferView` (`Uint8Array`, ...) and,
where noted, a `string` (encoded as UTF-8). Results are always fresh
`ArrayBuffer`s.

## Globals installed by the host

Besides `globalThis.__autable_host` the runtime defines these standard
globals, so ordinary npm code works:

| Global | Notes |
|---|---|
| `TextEncoder`, `TextDecoder` | UTF-8 only. `encode(string) → Uint8Array`, `decode(bytes) → string` (invalid sequences become U+FFFD). |
| `atob`, `btoa` | Latin-1 strings, as in browsers. |
| `URL`, `URLSearchParams` | WHATWG-style: `href`, `protocol`, `username`, `password`, `host`, `hostname`, `port`, `pathname`, `search`, `hash`, `origin`, `searchParams`, `toString()`; `URLSearchParams` has `get`, `getAll`, `set`, `append`, `delete`, `has`, `forEach`, `keys`, `values`, `entries`, `toString`, and is iterable. Parsing uses Go's `net/url`. |
| `setTimeout(fn, ms, ...args)`, `clearTimeout(id)` | Timers live inside the run only; they keep the run alive. |
| `console.log/info/warn/error/debug` | Map to the run log (`log` and `debug` → `info`). Arguments are joined with spaces; objects are JSON-encoded. |

No other host globals exist (`require`, `process`, Node built-ins and the
browser `fetch` are absent; the SDK's `fetch` is built on `__autable_host.fetch`).

## `globalThis.__autable_host`

### `version: string`

The host API version, `"1.0"` (same as the server's workflow SDK version).

### `info: object` (read-only)

```ts
{ database: string; workflow: string; version: number; runId: number; trigger: string }
```

While triggers are being read at publish time, `runId` is `0` and
`trigger` is `""`.

### `fetch(request) → Promise<response>` (async)

```ts
request: {
  url: string;                                   // absolute http(s) URL
  method?: string;                               // default "GET"
  headers?: Record<string, string> | [string, string][];
  body?: string | ArrayBuffer | ArrayBufferView; // string is sent as UTF-8
  timeoutMs?: number;                            // default 30000, max = time left in the run
  via?: string;                                  // runner name: the runner performs the request
}
response: {
  status: number;
  statusText: string;                            // e.g. "OK"
  url: string;                                   // final URL after redirects
  headers: [string, string][];                   // lower-case names sorted by name; repeated headers repeat in received order
  body: ArrayBuffer;
}
```

- Any HTTP status resolves; only transport failures, timeouts and the cap
  reject.
- Response bodies are capped at **20 MiB**: a larger body rejects with
  `response body exceeds 20 MiB`.
- Redirects are followed (at most 10), on the server and on runners alike.
- With `via`, the request is sent to the named runner of the workflow's
  database; it rejects with `runner "x" is not connected` when no such
  runner is online. The JSON-body convenience (`json:`) is implemented
  by the SDK (it sets `body` and `content-type`).

### Crypto (sync)

Algorithm names are lower-case strings.

| Function | Returns |
|---|---|
| `hash(alg, data)` — `alg`: `"sha1" \| "sha256" \| "sha512" \| "md5"`; `data`: bytes or string | `ArrayBuffer` digest |
| `hmac(alg, key, data)` — same algorithms; `key`, `data`: bytes or string | `ArrayBuffer` MAC |
| `randomBytes(n)` — `0 ≤ n ≤ 65536` | `ArrayBuffer` from the OS CSPRNG |
| `sign(alg, privateKeyPem, data)` | `ArrayBuffer` signature |
| `verify(alg, publicKeyPem, data, signature)` | `boolean` (`false` for a wrong signature; throws for a malformed key) |
| `publicKey(privateKeyPem)` | `string`: the matching public key as base64 SPKI DER, byte for byte what OpenSSL writes |
| `encrypt(alg, key, iv, data, aad?)` | `ArrayBuffer` |
| `decrypt(alg, key, iv, data, aad?)` | `ArrayBuffer` (throws on authentication failure / bad padding) |
| `timingSafeEqual(a, b)` | `boolean`, constant time for equal lengths |

- `sign`/`verify` algorithms: `"ed25519"`, `"ecdsa-p256-sha256"`,
  `"ecdsa-sha256-p1363"` and `"rsa-pkcs1-sha256"`.
  - `"ecdsa-p256-sha256"` signatures are produced in ASN.1 DER form (as
    OpenSSL does); `verify` accepts DER or the 64-byte raw `r‖s` form
    WebCrypto uses.
  - `"ecdsa-sha256-p1363"` is ECDSA over the SHA-256 digest on the curve
    the key names: P-256, P-384, P-521 or secp160r1 (any curve Go's x509
    parses also works). Signatures are IEEE P1363 raw `r‖s`, each half
    zero-padded to the byte length of the group order: 64, 96, 132 or 42
    bytes (secp160r1's order is 161 bits, so 21 bytes per half). This is
    the layout .NET's `ECDsa.SignData`/`VerifyData` and Windows CNG use by
    default; `verify` accepts only that layout and returns `false` for any
    other length. `sign` verifies the signature against the key's own
    public key before returning it. secp160r1 runs on Go's generic,
    non-constant-time ECDSA arithmetic: fine for a server issuing short
    codes (e.g. licence codes typed by hand), not for keys an attacker can
    time.
- Private keys are PEM `PRIVATE KEY` (PKCS #8), or `EC PRIVATE KEY` /
  `RSA PRIVATE KEY`; public keys are PEM `PUBLIC KEY` (SPKI) or
  `RSA PUBLIC KEY`; a PEM `CERTIFICATE` is also accepted for `verify`.
  Text that is not PEM is read as single-line base64 DER: PKCS #8 for
  private keys, SPKI for public keys (whitespace is ignored).
- `encrypt`/`decrypt` algorithms: `"aes-gcm"` (12-byte IV recommended;
  output is ciphertext followed by the 16-byte tag; `aad` optional) and
  `"aes-cbc"` (16-byte IV, PKCS #7 padding; `aad` not allowed). Key
  length selects AES-128/192/256.

### Encoding (sync)

| Function | Returns |
|---|---|
| `encode(encoding, bytes)` — `encoding`: `"base64" \| "base64url" \| "hex" \| "base58" \| "utf8"` | `string` (`base64url` without padding) |
| `decode(encoding, text)` | `ArrayBuffer` (`base64`/`base64url` accept padded and unpadded input; throws on invalid input) |

`base58` uses the Bitcoin alphabet
(`123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz`, no `0`,
`O`, `I`, `l`) and is **fixed-width**: the bytes are one big-endian integer
written in exactly `ceil(8n / log2 58)` digits for `n` bytes, left-padded
with `1` (the digit zero). The same number of bytes always gives the same
number of characters (20 → 28, 32 → 44, 42 → 58, 64 → 88), so a verifier
can check a code's length outright. `decode` maps each such width back to
its byte length and throws on any other length, on a character outside the
alphabet, and on a value too large for the width. Leading zero bytes are
not encoded as one `1` each as in Bitcoin's base58, so codes are not
interchangeable with other base58 implementations. Input is limited to
1024 bytes.

### Secrets and vars (sync)

| Function | Returns |
|---|---|
| `secret(name)` | `string`, or `undefined` when the secret is not set |
| `variable(name)` | `string`, or `undefined` when the var is not set |

Values are the ones current when the run started.

### Log (sync)

`log(level, message, data?)` — `level`: `"info" | "warn" | "error"`;
`message`: string; `data`: any JSON-able value (optional). Stored with the
run. Limits: 1000 entries per run (later entries are dropped and one
`warn` entry says so), messages truncated to 8 KiB, `data` over 64 KiB is
replaced by a note.

### `table(op, table, args) → Promise` (async)

Runs as the `workflow:<id>` subject through the same checks as the HTTP
API (`docs/permissions.md`). `table` is a table name of the workflow's
database. Rows are `{ record_id: number, values: object }`.

| `op` | `args` | Resolves to |
|---|---|---|
| `"list"` | `{ view?, query?, sorts?, search?, limit?, offset? }` — the body of `POST /api/tables/{db}/{table}/rows/query` | `Row[]` |
| `"page"` | same, `limit` required | `{ rows: Row[], total: number }` |
| `"create"` | `{ values }` | `Row` |
| `"update"` | `{ record_id, values }` | `Row` |
| `"upsert"` | `{ match_field, values }` | `{ operation: "create" \| "update" \| "noop", record_id, values }` |
| `"delete"` | `{ record_id }` | `Row` (its last values) |
| `"history"` | `{ record_id }` | `RowChange[]` (`{ history_key, database, table, record_id, timestamp, operation, values, diff, actor_id }`), oldest first |

`query` is a group `{ combinator: "and" | "or", rules: [...], not? }` of
conditions `{ field, operator, value? }`, exactly as in the app SDK.

### `users(op, args) → Promise` (async)

| `op` | `args` | Resolves to |
|---|---|---|
| `"get"` | `{ id }` | `{ id, email, display_name, provider }` or `null` |
| `"byEmail"` | `{ email }` | same, or `null` |
| `"search"` | `{ query?, limit? }` (limit ≤ 100, default 20) | `User[]` |

### `files(op, args) → Promise` (async)

As the workflow subject, with the row-level file rules of
`docs/permissions.md`. `FileRecord` is the app SDK's shape
(`{ id, name, size, content_type, database_name, table_name, record_id, created_at, creator_id? }`).

| `op` | `args` | Resolves to |
|---|---|---|
| `"metadata"` | `{ ids: number[] }` | `FileRecord[]` (invisible ids left out) |
| `"read"` | `{ id }` | `{ file: FileRecord, body: ArrayBuffer }` (20 MiB cap) |
| `"upload"` | `{ table, name, body, content_type?, record_id? }` | `FileRecord` — store its `id` in a file field with `table("create"/"update")`, which binds it |

`files` rejects with `file storage is not configured` when the server has
no S3 storage.

## Reading triggers (publish)

On publish the server evaluates the bundle once in a runtime where every
`__autable_host` function throws
`host calls are not available while reading triggers` and timers do not
fire, with a 5 s limit. It then reads `__workflow.triggers` (a plain
object: trigger name → declaration) and checks that `__workflow.run` is a
function. Names match `^[A-Za-z_][A-Za-z0-9_]{0,63}$`. The SDK's helpers
must produce exactly these declaration objects:

| Helper | Declaration |
|---|---|
| `schedule({ cron, timezone? })` | `{ kind: "schedule", cron: string, timezone?: string }` — 5 fields (minute hour day-of-month month day-of-week); `*`, lists, ranges, `*/n` intervals, `JAN`–`DEC`, `SUN`–`SAT`, `0` or `7` = Sunday. Day-of-month and day-of-week combine with OR when both are restricted, as in cron. `timezone` is an IANA name, default `"UTC"`. |
| `recordChanged({ table, operations?, fields?, includeOwnWrites? })` | `{ kind: "recordChanged", table: string, operations?: ("create" \| "update" \| "delete")[], fields?: string[], includeOwnWrites?: boolean }` — `operations` defaults to all three; with `fields`, only changes that touch one of those fields fire; `includeOwnWrites` defaults to `false`, so changes made by this workflow's own runs never fire it. The table and fields must exist. |
| `webhook({ path })` | `{ kind: "webhook", path: string }` — segments of `[A-Za-z0-9._~-]`, separated by `/`; unique per workflow |
| `manual({ fields? })` | `{ kind: "manual", fields?: { name: string, label?: string, type: "string" \| "number" \| "boolean" }[] }` |

Unknown keys are rejected so typos fail the publish (422) instead of being
ignored.

## The `event` passed to `run`

Common fields: `{ trigger: string, runId: number }`, plus per kind:

| Kind | Fields |
|---|---|
| schedule | `scheduledAt: number` — the matched minute, ms since the epoch |
| recordChanged | `change: RowChange & { actor: { id, type: "user" \| "workflow", email?, display_name? }, changedFields: string[] }` — `RowChange` as in `table("history")` (`values` are the row after the change, before it for deletes; `diff` maps each changed field to `{ old, new }`); `values`, `diff` and `changedFields` only contain fields the workflow subject may read. `actor.id` is a user id or `workflow:<id>`. |
| webhook | `request: { method, path, url, query, headers, body }` — `path` is the part after `/api/hooks/{db}/{name}/`; `url` is the full request URI (path and query); `query` is `Record<string, string[]>`; `headers` is `Record<string, string>` with lower-case names, repeated headers joined with `", "`; `body` is an `ArrayBuffer` (at most 5 MiB) |
| manual | `input: object` (checked against the declared `fields`: declared fields that are present must have the declared type); `startedBy?: { id, email, display_name }` when a user started the run |

## The value `run` returns

- Non-webhook runs: the resolved value is stored as the run's `output`
  after `JSON.stringify` semantics (undefined → no output). It is capped at
  1 MiB; a larger output fails the run.
- Webhook runs answer the HTTP request with it:
  - `undefined` → `204 No Content`;
  - otherwise it must be `{ status: number, headers?: Record<string, string>, body?: string | ArrayBuffer | ArrayBufferView | any }`:
    strings are sent as-is (`text/plain; charset=utf-8` unless
    `headers` sets a content type), bytes raw (`application/octet-stream`
    by default), anything else JSON-encoded (`application/json`).
  - A thrown error or an invalid return answers `500` with
    `{"error": "...", "run_id": N}`; exceeding 15 s answers `504`.
  - The stored run `output` is `{ status, headers }` (the body is not stored).
