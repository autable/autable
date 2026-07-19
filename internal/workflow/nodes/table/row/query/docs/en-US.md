## Query rows

Queries table rows through the server table API. The workflow creator permissions are used for access checks.

This node uses the same query shape as the public `POST /api/tables/{database}/{table}/rows/query` API.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `view` (`string`): Optional view name. View filters and sorts are applied before runtime options.
- `query` (`object`): Optional query object. Use a full `ViewQuery`, or the shorthand `{ field, op/operator, value }`.
- `sorts` (`object[]`): Optional sort definitions, for example `{ field: "name", direction: "asc" }`.
- `limit` (`int`): Optional maximum number of rows.
- `offset` (`int`): Optional number of rows to skip, for pagination. Requires a positive `limit`.
- `search` (`string`): Optional free-text term matched as "contains" against every readable text/number/formula field (relation and file fields excluded), AND-combined with `query`.

### Outputs

- `rows` (`RowRecord[]`): Matching rows.

### Example

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { query_contacts: "table.row.query" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  const result = info.instance("query_contacts").exec({
    table: "contacts",
    query: { field: "email", operator: "=", value: "ada@example.com" },
    limit: 1
  });
  return { count: result.rows.length, rows: result.rows };
}
```
