## 创建记录

通过后端 table API 创建一行记录。权限使用 workflow 创建者的权限。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `values` (`object`): 要写入的字段值。

### 输出

- `record` (`RowRecord`): 创建后的记录，包含 `record_id` 和 `values`。

### 示例

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { create_contact: "table.row.create" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("create_contact").exec({
    table: "contacts",
    values: { name: "Ada" }
  });
}
```
