## 查询记录

通过后端 table API 查询记录。权限使用 workflow 创建者的权限。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `view` (`string`): 可选视图名。视图的 filter 和 sort 会由 table service 应用。

### 输出

- `rows` (`RowRecord[]`): 匹配的记录列表。

### 示例

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { list_contacts: "table.row.list" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  const result = info.instance("list_contacts").exec({
    table: "contacts",
    view: "all"
  });
  return { count: result.rows.length, rows: result.rows };
}
```
