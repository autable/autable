## 更新或创建记录

用 `match_field` 和 `values[match_field]` 查找第一条匹配记录。找到则更新，找不到则新建。如果匹配记录的字段值已经一致，会返回 `noop`，不会写入 row history。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `match_field` (`string`): 用来查找已有记录的字段。
- `values` (`object`): 要写入的字段值，必须包含 `match_field`。

### 输出

- `record` (`RowRecord`): 更新或创建后的记录。
- `operation` (`string`): `update`、`create` 或 `noop`。

### 示例

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
