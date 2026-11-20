# Reading and writing rows

This is the reference for row data in autable: field types and their JSON
values, the query language, views, paging, upsert, files, and history. The
same operations are reachable four ways, all with the caller's own
permissions:

| Client | Where |
|---|---|
| HTTP | `/api/tables/{database}/{table}/rows...` (JSON bodies) |
| CLI | `autablectl rows ...` (see [cli.md](cli.md)) |
| App SDK | `table(name)` and `files` from the vendored `src/autable/autable.ts` |
| Workflow SDK | `tables(name)` and `files` from the vendored `./autable` |

The examples use a database `crm` with these tables:

```yaml
- name: contacts
  fields:
    - { name: name, type: string }
    - { name: email, type: string }
    - { name: status, type: string, options: [open, won, lost] }
    - { name: tags, type: string, options: [vip, partner, new], multiple: true }
    - { name: qty, type: int }
    - { name: price, type: float }
    - { name: total, type: formula, value_type: float, formula: "fields.qty * fields.price" }
    - { name: company, type: relation, relation_table: companies }
    - { name: attachment, type: file }
    - { name: owner_email, type: string }
```

## Rows

Every row read or returned by a write has this shape:

```json
{ "record_id": 42, "values": { "name": "Ada", "qty": 3, "price": 2.5, "total": 7.5, "company": 7, "attachment": null } }
```

- `record_id` is the row's id (a positive integer assigned on create).
- `values` holds every active (non-deleted) field the caller may read. An
  empty cell is `null`. A field the caller may not read is missing from
  `values` altogether.
- Field names are case-sensitive in requests. Unknown or deleted field names
  in `values`, a query, or a sort are rejected with `400`.

## Field types

The `type` values a table field can have are exactly `int`, `float`,
`string`, `formula`, `relation`, and `file`. There are no dedicated date,
boolean, user, or JSON types; such data lives in these types (for instance
an email address in a `string` field, which is what `$current_user`
compares against, see [Views](#views)).

| Type | Read (JSON) | Write accepts | Stored as |
|---|---|---|---|
| `int` | number or `null` | integral number (`3`, `3.0`), numeric string (`"42"`, trimmed), `null`, `""` | integer |
| `float` | number or `null` | number, numeric string, `null`, `""` | real |
| `string` | string or `null` | any value, converted to text; `null` | text |
| `string` + `options` | one option, `""`, or `null` | one of `options`, `""`, `null` | text |
| `string` + `options` + `multiple` | `"a,b"` or `""`/`null` | options joined by `,` | text |
| `formula` | per `value_type` | read-only: any write is rejected (`403`) | per `value_type` |
| `relation` | record id (number) or `null` | record id like `int` | integer |
| `file` | file id (number) or `null` | positive file id, `null`, `""` | integer |

Rules derived from the server's normalization:

- **Empty means null.** For every non-string type, `""` or a whitespace-only
  string is stored as `null`. `null` clears any writable field.
- **int**: a non-integral number (`3.5`) or unparsable string is an error
  (`field "qty": ...`, `400`). Booleans, arrays, and objects are errors.
- **float**: numbers and numeric strings; anything else is an error. An
  integral float is returned as a JSON integer (`2.0` reads back as `2`).
- **string**: non-string values are converted with Go's default formatting:
  `3` becomes `"3"`, `true` becomes `"true"`. Arrays and objects are *not*
  JSON-encoded; to keep structured data in a string field, send it already
  serialized (`JSON.stringify`).
- **Options (enum)**: `options` is only allowed on `string` fields. A write
  must be one of the listed options; `""` and `null` are always allowed.
  Comparison is exact (case- and space-sensitive).
- **Multiple options**: `multiple: true` requires `options`. The value is the
  chosen options joined by `,` with no spaces (`"vip,new"`), in the order
  given; every part must be an option and none may repeat. Options of a
  multi-valued field cannot contain `,`. The SDKs export
  `splitOptions(value)` and `joinOptions(list)` for this.
- **relation**: holds the `record_id` of a row in `relation_table` (same
  database). The value is returned as the bare id, not expanded; fetch the
  linked row with a query on `ct_record_id` in the target table. The server
  does not check that the target row exists.
- **file**: holds the id returned by an upload; see [Files](#files).
- **formula**: see below.

### Formula fields

A formula field has `formula` (a JavaScript expression) and `value_type`
(`int`, `float`, or `string`; it cannot change later). The server evaluates
the expression and stores the result:

- In scope: `fields` (the row's current values by field name), `record_id`,
  `now` (milliseconds since the Unix epoch, UTC), and `today` (UTC midnight
  of the current day, in milliseconds).
- Example: `fields.qty * fields.price`, or
  `fields.status === "won" ? "closed" : "active"` with `value_type: string`.
- Formulas run in field order on every create and update of the row, so a
  formula sees the new values of formulas defined before it. When a formula
  definition changes, every row of the table is recomputed. `now` and
  `today` are the time of that computation, not of the read.
- The result is converted to `value_type` like a write (a non-integral result
  for `int` fails). Any evaluation or conversion error stores `null` instead
  of failing the write.
- Formula values are stored, so they can be filtered and sorted like the
  `value_type` they have.

## System columns

| Name | Visible | Query / sort | Writable |
|---|---|---|---|
| `ct_record_id` | as `record_id` on every row | yes, as field `ct_record_id` (an `int`) | no |
| `ct_created_at`, `ct_updated_at` | no (stored, milliseconds UTC) | no (`unknown view query field`) | no |

There is no created-by or updated-by column; who changed a row and when is
in its [history](#history). Field names starting with `ct_` are reserved.
Referencing `ct_record_id` in a query or sort needs no field grant.

## Queries

A query is a group:

```json
{ "combinator": "and", "rules": [ <rule or group>, ... ], "not": false }
```

- `combinator`: `"and"` or `"or"`.
- `rules`: conditions `{ "field", "operator", "value" }` and nested groups.
  An entry with `combinator` or `rules` is a group; anything else is a
  condition.
- `not` (optional): negates the whole group (`NOT (...)`).
- A group with no rules adds no condition.
- The top level of a request must be a group. The CLI and both SDKs accept a
  single condition and wrap it in an `and` group.

Nested example: open contacts whose name contains "ad" or who have a qty of
at least 2, excluding one email:

```json
{
  "combinator": "and",
  "rules": [
    { "field": "status", "operator": "=", "value": "open" },
    { "combinator": "or", "rules": [
      { "field": "name", "operator": "contains", "value": "ad" },
      { "field": "qty", "operator": ">=", "value": 2 }
    ]},
    { "combinator": "and", "not": true, "rules": [
      { "field": "email", "operator": "=", "value": "ada@example.com" }
    ]}
  ]
}
```

### Operators

The value of `=`, `!=`, `<`, `<=`, `>`, `>=` is converted to the field's
storage type first:

- `int`, `relation`, `file`, `ct_record_id`, int formulas: a number or a
  numeric string. A fractional number is truncated (`2.7` compares as `2`).
  Anything else (including a missing value) is a `400` error.
- `float`, float formulas: a number or numeric string.
- `string`, string formulas: the value's text form (`3` becomes `"3"`).
  Comparison is exact and case-sensitive; `<`/`>` compare text, not numbers.
  A missing value does **not** match empty cells; use `null` for that.

The text operators work on every type: the column is cast to text and both
sides are lower-cased, so they are case-insensitive (SQLite `LOWER`, which
folds ASCII letters). `%` and `_` in the value are literal. The value is any
scalar; there are no list or range values (combine conditions instead).

| Operator | Meaning | Example |
|---|---|---|
| `=` | equal | `{"field":"status","operator":"=","value":"open"}` |
| `!=` | not equal (rows with a `null` cell do not match) | `{"field":"status","operator":"!=","value":"lost"}` |
| `<` | less than | `{"field":"price","operator":"<","value":10}` |
| `<=` | less than or equal | `{"field":"qty","operator":"<=","value":"5"}` |
| `>` | greater than | `{"field":"ct_record_id","operator":">","value":100}` |
| `>=` | greater than or equal | `{"field":"total","operator":">=","value":7.5}` |
| `contains` | text contains | `{"field":"name","operator":"contains","value":"ad"}` |
| `doesNotContain` | text does not contain; `null` cells match | `{"field":"email","operator":"doesNotContain","value":"@example.com"}` |
| `beginsWith` | text starts with | `{"field":"name","operator":"beginsWith","value":"A"}` |
| `doesNotBeginWith` | text does not start with; `null` cells match | `{"field":"name","operator":"doesNotBeginWith","value":"test"}` |
| `endsWith` | text ends with | `{"field":"email","operator":"endsWith","value":"@example.com"}` |
| `doesNotEndWith` | text does not end with; `null` cells match | `{"field":"email","operator":"doesNotEndWith","value":".test"}` |
| `null` | cell is `null` or blank text; no value | `{"field":"company","operator":"null"}` |
| `notNull` | cell is neither `null` nor blank; no value | `{"field":"attachment","operator":"notNull"}` |

Notes:

- Multi-valued option fields are stored as `"a,b"`: `contains` matches a
  substring of that text (`"vip"` also matches an option named `"vip2"`),
  and `=` matches the exact joined string.
- `null`/`notNull` treat `""` and whitespace as empty.
- Any string value that is exactly `"$current_user"` is replaced by the
  caller's identity, in request queries as well as in views (see below).

### Sorts

```json
[ { "field": "status", "direction": "asc" }, { "field": "ct_record_id", "direction": "desc" } ]
```

`direction` is `asc` or `desc`; `field` is any active field or
`ct_record_id`. Sorts apply in order, and `ct_record_id` ascending is
always the final tie-breaker; without sorts rows come in `ct_record_id`
order.

### Search

`search` (a string) adds an `or` group of `contains` conditions over every
readable `int`, `float`, `string`, and `formula` field; `relation` and
`file` fields are not searched. It is combined with the rest of the query by
`and`. If the caller can read no searchable field, the result is empty.

## Views

Views are stored in the table metadata (`GET /api/metadata`,
`autablectl meta DATABASE TABLE`):

```json
{
  "name": "mine",
  "display_name": "My contacts",
  "base_view": "",
  "query": { "combinator": "and", "rules": [ { "field": "owner_email", "operator": "=", "value": "$current_user" } ] },
  "sorts": [ { "field": "name", "direction": "asc" } ]
}
```

- A view stores a query and sorts only (no column list).
- `base_view` names another view of the same table: the effective query is
  the base view's query `and` this view's query, and the sorts are the base
  view's followed by this view's. Cycles are rejected.
- `all` is the built-in unfiltered view; it is not stored and the name is
  reserved. A request without `view` uses `all`.
- Views are the row-level permission boundary. Reading through a view needs
  a read grant on that view (`all` included) and read access to every field
  its query and sorts use; otherwise `403`. The rows a caller can get,
  update, delete, or see the history and files of are the union of the rows
  of every view they may read.
- `$current_user` resolves to the caller's email address. For a subject
  without a user account (such as a workflow, `workflow:<id>`) it resolves to
  that subject id, which normally matches no rows.

A request's own parameters combine with the view like this:

| Request parameter | Effect |
|---|---|
| `query` | `and`-ed with the view's query (cannot widen the view) |
| `search` | `and`-ed as well |
| `sorts` | if non-empty, **replaces** the view's sorts |
| `limit`, `offset` | applied after filtering and sorting |

## Paging

Paging is limit/offset; there is no cursor.

- `limit`: maximum rows. `0` or absent means no limit; the server sets no
  default and no maximum. Negative values are rejected.
- `offset`: rows to skip; requires a positive `limit`.
- `POST .../rows/page` returns `{ "rows": [...], "total": N }`, where `total`
  counts the rows matching view, query, and search, ignoring limit and
  offset. `POST .../rows/query` returns only the array.
- `autablectl rows list` defaults to `--limit 100` and always uses `page`.
  The SDKs' `page()` requires `limit`; the workflow SDK rejects a
  non-positive one.

## Writes

- **Create** sets the given fields; omitted fields are `null`. Needs the
  record create grant and a create grant on each field written.
- **Update** changes only the given fields; the others keep their values.
  Needs an update grant on each field written, and the row must be in the
  caller's row set (else `404`).
- **Delete** needs the record delete grant and the row in the row set; the
  response is the row's last values.
- Writes to `ct_record_id` or a formula field are rejected (`403`); other
  `ct_` names are unknown fields (`400`). Responses contain only fields the
  caller may read.

### Upsert

Body: `{ "match_field": "email", "values": { "email": "ada@example.com", "name": "Ada" } }`.

- `match_field` must be a field of the table (not `ct_record_id`), and
  `values` should contain it; the workflow SDK rejects a call without it.
- The server looks for rows where `match_field` `=` the given value, as a
  list over the built-in `all` view, so the caller needs read access to
  `all` and to the match field. Rows outside it count as absent.
- **No match**: creates a row from `values`; `operation` is `"create"`.
- **Match**: takes the first matching row in `ct_record_id` order (with
  several matches, the oldest); the others are left alone. If every given
  value already equals the stored one (compared as text after type
  conversion), nothing is written and no history is recorded: `"noop"`.
  Otherwise the given fields are updated: `"update"`.
- Response (`200` in every case):
  `{ "operation": "create" | "update" | "noop", "record_id": 42, "values": {...} }`.

## Files

1. Upload: `POST /api/files` as `multipart/form-data` with fields `file`,
   `database_name`, `table_name`, and optionally `record_id`. The caller
   needs file access on the table. The default size limit is 20 MiB
   (`413` above it). Response `201`:

   ```json
   { "id": 15, "name": "spec.pdf", "size": 48213, "content_type": "application/pdf", "creator_id": "...", "database_name": "crm", "table_name": "contacts", "record_id": 0, "created_at": 1767225600000 }
   ```

2. Store the `id` in a file field with create, update, or upsert. This binds
   the file to that row, once and for good: the file must have been uploaded
   for the same database and table, a file bound to another row cannot be
   moved, and only the uploader can bind an unbound file. Uploading with
   `record_id` binds it to that row immediately.
3. The cell then reads as the number `15`. `POST /api/files/metadata` with
   `{ "ids": [15, 16] }` returns the records of the files the caller may see
   (others are left out).
4. Download: `GET /api/files/{id}`. Only bound files can be downloaded, by
   callers with file access on the table whose row set contains the row.
   PDFs and PNG/JPEG/GIF/WebP/BMP/AVIF images are served inline, everything
   else as an attachment. Unauthenticated browser requests are redirected to
   the login page; API clients get `401`.

App SDK: `files.upload(blob, { table, recordId?, name? })`, `files.url(id)`,
`files.metadata(ids)`. Workflow SDK: `files.upload({ table, name, body,
contentType?, recordId? })`, `files.read(id)` (at most 20 MiB),
`files.metadata(ids)`.

## History

`GET /api/tables/{db}/{table}/rows/{id}/history` returns every change of the
row, oldest first:

```json
[
  {
    "history_key": "rhistory_crm_contacts_...",
    "database": "crm", "table": "contacts", "record_id": 42,
    "timestamp": 1767225600000,
    "operation": "update",
    "values": { "name": "Ada", "qty": 4 },
    "diff": { "qty": { "old": 3, "new": 4 } },
    "actor_id": "..."
  }
]
```

`operation` is `create`, `update`, or `delete`; `values` is the row after the
change (before it, for a delete); `timestamp` is milliseconds since the Unix
epoch; `actor_id` is the subject that made the change. Values and diffs are
limited to readable fields, and the row must be in the caller's row set
(a non-admin gets `404` for a deleted row).

## HTTP reference

| Method and path | Body | Response |
|---|---|---|
| `GET /api/tables/{db}/{table}/rows?view=V&sort_field=F&sort_direction=asc` | none | `[row, ...]` (no query or paging) |
| `POST /api/tables/{db}/{table}/rows/query` | list body | `[row, ...]` |
| `POST /api/tables/{db}/{table}/rows/page` | list body | `{ "rows": [...], "total": N }` |
| `POST /api/tables/{db}/{table}/rows` | `{ "values": {...} }` | `201` row |
| `PATCH /api/tables/{db}/{table}/rows/{id}` | `{ "values": {...} }` | row |
| `POST /api/tables/{db}/{table}/rows/upsert` | `{ "match_field", "values" }` | `{ "operation", "record_id", "values" }` |
| `DELETE /api/tables/{db}/{table}/rows/{id}` | none | last row values |
| `GET /api/tables/{db}/{table}/rows/{id}/history` | none | `[change, ...]` |

List body (every key optional):
`{ "view", "query", "sorts", "search", "limit", "offset" }`. There is no
single-row GET; query `ct_record_id` with `limit: 1`.

- JSON bodies need `Content-Type: application/json` (`415` otherwise)
  unless the request uses `Authorization: Bearer <token>`. Unknown body keys
  are rejected.
- Errors are `{ "error": "message" }` with `400` (invalid input), `401`
  (not signed in), `403` (permission), or `404` (no such row, or outside the
  caller's row set).

```sh
curl -X POST https://autable.example.com/api/tables/crm/contacts/rows/page \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"query":{"combinator":"and","rules":[{"field":"status","operator":"=","value":"open"}]},
       "sorts":[{"field":"ct_record_id","direction":"desc"}],"limit":20,"offset":40}'
```

## CLI

JSON flags take inline JSON, `@file`, or `-` for stdin.

```sh
autablectl rows list crm contacts --query '{"field":"status","operator":"=","value":"open"}' --limit 20 --offset 40
autablectl rows list crm contacts --view mine --search ada --sorts '[{"field":"name","direction":"asc"}]'
autablectl rows get crm contacts 42
autablectl rows create crm contacts --values '{"name":"Ada","status":"open","tags":"vip,new","qty":3}'
autablectl rows update crm contacts 42 --values '{"qty":4,"company":7}'
autablectl rows upsert crm contacts --match email --values '{"email":"ada@example.com","name":"Ada"}'
autablectl rows delete crm contacts 42
autablectl rows history crm contacts 42
```

## App SDK

```ts
import { table, files, joinOptions } from "./autable/autable";

const contacts = table("contacts");
const open = await contacts.query(
  { combinator: "or", rules: [
    { field: "status", operator: "=", value: "open" },
    { field: "qty", operator: ">=", value: 2 },
  ]},
  { sorts: [{ field: "ct_record_id", direction: "desc" }], limit: 50 },
);
const { rows, total } = await contacts.page({ view: "mine", limit: 20, offset: 40 });
const one = await contacts.get(42); // null when missing or not visible
const created = await contacts.create({ name: "Ada", tags: joinOptions(["vip", "new"]) });
await contacts.update(created.record_id, { qty: 4 });
const { operation } = await contacts.upsert("email", { email: "ada@example.com", name: "Ada" });

const uploaded = await files.upload(fileInput.files![0], { table: "contacts" });
await contacts.update(created.record_id, { attachment: uploaded.id });
const href = files.url(uploaded.id);
```

`list(options)` takes `{ filter, sorts, view, search, limit, offset }`;
`filter` may be a single condition or a group. Failed calls throw
`AutableError` with `status` and the server's message.

## Workflow SDK

```ts
import { tables, files } from "./autable";

const contacts = tables("contacts");
const stale = await contacts.list({
  filter: { combinator: "and", rules: [
    { field: "status", operator: "=", value: "open" },
    { field: "company", operator: "null" },
  ]},
  limit: 100,
});
for (const row of stale) {
  await contacts.update(row.record_id, { status: "lost" });
}
const result = await contacts.upsert("email", { email: "ada@example.com", qty: 1 });
const history = await contacts.history(result.record_id);

const report = await files.upload({ table: "contacts", name: "report.csv", body: "a,b\n1,2\n", recordId: result.record_id });
await contacts.update(result.record_id, { attachment: report.id });
```

Workflow calls run as the `workflow:<id>` subject with only its grants. A
permission failure rejects with an Error named `PermissionError`; a row
outside its row set with one named `NotFoundError`.
