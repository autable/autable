## List rows

Lists table rows through the server table API. The workflow creator permissions are used for access checks.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `view` (`string`): Optional view name. View filters and sorts are applied by the table service.

### Outputs

- `rows` (`RowRecord[]`): Matching rows.

### Example

```js
function instances(info) {
  return { list_contacts: "table.row.list" };
}

function run(info) {
  const result = info.instance("list_contacts").exec({
    table: "contacts",
    view: "all"
  });
  return { count: result.rows.length, rows: result.rows };
}
```
