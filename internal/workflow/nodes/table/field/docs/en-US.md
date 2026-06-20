## Create table fields

Adds missing fields to a table through the server metadata API. Existing fields are returned as `existing`, so this node is safe to call before every sync batch.

### Inputs

- `database` (`string`): Optional database name. Defaults to the workflow database.
- `table` (`string`): Target table name.
- `fields` (`string[] | object[] | object`): Fields to ensure. A string creates a `string` field. An object supports `{ name, type }`. A map such as `{ title: "string", score: "float" }` is also accepted.

Supported field types are `string`, `int`, and `float`.

### Outputs

- `created` (`Field[]`): Fields added by this call.
- `restored` (`Field[]`): Soft-deleted fields restored by this call.
- `existing` (`Field[]`): Fields that already existed.
- `fields` (`Field[]`): Active fields after the update.

### Example

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { ensure_fields: "table.field.create" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("ensure_fields").exec({
    table: "contacts",
    fields: [
      "name",
      { name: "score", type: "float" }
    ]
  });
}
```
