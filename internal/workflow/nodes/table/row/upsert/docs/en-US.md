## Update or create row

Updates the first row whose `match_field` value equals `values[match_field]`. If no row matches, creates a new row. If the matched row already has the same values, it returns `noop` without writing row history.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `match_field` (`string`): Field used to find the existing row.
- `values` (`object`): Values to write. Must include `match_field`.

### Outputs

- `record` (`RowRecord`): The updated or created record.
- `operation` (`string`): `update`, `create`, or `noop`.

### Example

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { upsert_contact: "table.row.upsert" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("upsert_contact").exec({
    table: "contacts",
    match_field: "external_id",
    values: {
      external_id: "remote-1",
      name: "Ada"
    }
  });
}
```
