## Update row

Updates one row through the server table API. The workflow creator permissions are used for access checks.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `record_id` (`int64`): Record id to update.
- `values` (`object`): Field values to overwrite.

### Outputs

- `record` (`RowRecord`): Updated row with `record_id` and `values`.

### Example

```js
function instances(info) {
  return { update_contact: "table.row.update" };
}

function run(info) {
  return info.instance("update_contact").exec({
    table: "contacts",
    record_id: info.inputs.record_id,
    values: { status: "Done" }
  });
}
```
