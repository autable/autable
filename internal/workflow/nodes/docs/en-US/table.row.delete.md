## Delete row

Deletes one row through the server table API. The workflow creator permissions are used for access checks.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `record_id` (`int64`): Record id to delete.

### Outputs

- `record` (`RowRecord`): Deleted row snapshot.

### Example

```js
/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | CodeTableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { delete_contact: "table.row.delete" };
}

/**
 * @param {CodeTableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("delete_contact").exec({
    table: "contacts",
    record_id: info.inputs.record_id
  });
}
```
