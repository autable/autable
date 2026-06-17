## Create row

Creates one row through the server table API. The workflow creator permissions are used for access checks.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `values` (`object`): Field values to write.

### Outputs

- `record` (`RowRecord`): Created row with `record_id` and `values`.

### Example

```js
function instances(info) {
  return { create_contact: "table.row.create" };
}

function run(info) {
  return info.instance("create_contact").exec({
    table: "contacts",
    values: { name: "Ada" }
  });
}
```
