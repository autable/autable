## 更新记录

通过后端 table API 更新一行记录。权限使用 workflow 创建者的权限。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `record_id` (`int64`): 要更新的记录 id。
- `values` (`object`): 要覆盖写入的字段值。

### 输出

- `record` (`RowRecord`): 更新后的记录，包含 `record_id` 和 `values`。

### 示例

```js
/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | CodeTableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { update_contact: "table.row.update" };
}

/**
 * @param {CodeTableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("update_contact").exec({
    table: "contacts",
    record_id: info.inputs.record_id,
    values: { status: "Done" }
  });
}
```
