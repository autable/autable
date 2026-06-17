## Record changed

Trigger node for row history events. The trigger input is the `params` object returned by `trigger(info)`. When a backend row event matches those params, this node output becomes `run(info).inputs`.

Do not call this trigger node from `run(info)`. The backend runs it before starting the workflow run.

### Trigger params

- `table` (`string`): Optional table name. Empty means every table in the workflow database.
- `operations` (`string[]`): Optional filter. Supported values are `create`, `update`, and `delete`.
- `fields` (`string[]`): Optional changed-field filter. A create or update event matches when at least one changed field is listed here.

### Run inputs

- `history_key` (`string`): LevelDB key for the row history event.
- `database` (`string`): Database name.
- `table` (`string`): Table name.
- `record_id` (`int64`): Record id.
- `operation` (`string`): `create`, `update`, or `delete`.
- `record` (`TriggerRecord`): Compact record reference.
- `values` (`object`): Current row values from the event.
- `diff` (`object`): Changed fields keyed by field name, each with `old` and `new`.
- `actor_id` (`string`): User id that caused the row change.

### Example

```js
/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | CodeTableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { row_change: "table.record.changed" };
}

/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {CodeTableWorkflowTriggerDeclaration}
 */
function trigger(info) {
  return {
    instance: "row_change",
    params: {
      table: "contacts",
      operations: ["create", "update"],
      fields: ["status"]
    }
  };
}

/**
 * @param {CodeTableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return {
    record_id: info.inputs.record_id,
    operation: info.inputs.operation,
    diff: info.inputs.diff
  };
}
```
